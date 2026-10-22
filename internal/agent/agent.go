// Package agent implements the think-act loop: stream the LLM, split output
// into speakable sentences, execute tool calls and feed results back.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

// ErrMaxSteps is returned when the LLM keeps requesting tools past MaxSteps.
var ErrMaxSteps = errors.New("agent: max steps exceeded")

// Options configures a Runtime.
type Options struct {
	Provider     llm.Provider
	Tools        *tools.Registry // optional
	Store        *store.Store    // optional; nil = in-memory history
	SystemPrompt string          // default DefaultSystemPrompt
	MaxSteps     int             // max LLM calls per Run; default 6
	Logger       *slog.Logger
	Model        string
	Temperature  *float64
	// ToolTimeout bounds each tool call (default 30s).
	ToolTimeout time.Duration
	// Tracer receives trace rows and debug payloads (optional).
	Tracer Tracer
}

// Input is one user turn.
type Input struct {
	ConversationID string // empty = new conversation
	Channel        string // web|cli|speaker (default cli)
	Text           string
	// Observer (optional) receives live LLM step boundaries.
	Observer *Observer
}

// Observer receives live LLM step boundaries of a turn (the speaker hook
// turns them into timeline events). Callbacks must not block.
type Observer struct {
	LLMStart func(step int, model string)
	// LLMEnd gets the llm trace row of the step (durations, tokens, error).
	LLMEnd func(step int, row store.TraceRow)
}

// Runtime runs agent turns. It is safe for concurrent use.
type Runtime struct {
	opt Options
	log *slog.Logger

	mu     sync.Mutex
	mem    map[string][]llm.Message // used when Store == nil
	active map[*activeTurn]struct{} // turns currently running
}

// activeTurn is a running turn of one conversation.
type activeTurn struct {
	conv   string
	cancel context.CancelFunc
	done   chan struct{}
}

// cancelWait bounds how long CancelConversation waits for turns to stop.
const cancelWait = 5 * time.Second

// CancelConversation cancels the turns currently running for the conversation
// (waiting briefly for them to finish persisting), drops any in-memory
// history of it, and reports whether a turn was running.
func (r *Runtime) CancelConversation(id string) bool {
	return r.cancelTurns(func(t *activeTurn) bool { return t.conv == id }, id)
}

// CancelAll cancels every running turn and clears the in-memory history; it
// reports whether any turn was running.
func (r *Runtime) CancelAll() bool {
	return r.cancelTurns(func(*activeTurn) bool { return true }, "")
}

func (r *Runtime) cancelTurns(match func(*activeTurn) bool, memID string) bool {
	r.mu.Lock()
	var hit []*activeTurn
	for t := range r.active {
		if match(t) {
			hit = append(hit, t)
		}
	}
	if memID == "" {
		clear(r.mem)
	} else {
		delete(r.mem, memID)
	}
	r.mu.Unlock()
	for _, t := range hit {
		t.cancel()
	}
	deadline := time.After(cancelWait)
	for _, t := range hit {
		select {
		case <-t.done:
		case <-deadline:
		}
	}
	return len(hit) > 0
}

func (r *Runtime) register(conv string, cancel context.CancelFunc) (unregister func()) {
	t := &activeTurn{conv: conv, cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	r.active[t] = struct{}{}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.active, t)
		r.mu.Unlock()
		close(t.done)
	}
}

// New creates a Runtime, applying defaults.
func New(opt Options) *Runtime {
	if opt.SystemPrompt == "" {
		opt.SystemPrompt = DefaultSystemPrompt
	}
	if opt.MaxSteps <= 0 {
		opt.MaxSteps = 6
	}
	if opt.ToolTimeout <= 0 {
		opt.ToolTimeout = 30 * time.Second
	}
	lg := opt.Logger
	if lg == nil {
		lg = slog.New(slog.DiscardHandler)
	}
	return &Runtime{opt: opt, log: lg, mem: map[string][]llm.Message{}, active: map[*activeTurn]struct{}{}}
}

// Run executes one user turn, emitting events as they occur. It returns a
// non-nil error on provider/store failure, ErrMaxSteps, or ctx cancellation.
func (r *Runtime) Run(ctx context.Context, in Input, emit func(Event)) error {
	ch := in.Channel
	if ch == "" {
		ch = store.ChannelCLI
	}
	ts := &turnState{id: logx.NewID(), channel: ch, userText: in.Text, convID: in.ConversationID, start: time.Now()}
	ctx = logx.WithTurnID(ctx, ts.id)
	err := r.run(ctx, in, emit, ts)
	r.finishTurn(ctx, ts, err)
	return err
}

func (r *Runtime) run(ctx context.Context, in Input, emit func(Event), ts *turnState) error {
	if emit == nil {
		emit = func(Event) {}
	}
	start := ts.start
	log := r.log.With("turn_id", ts.id)
	// persistence must survive cancellation
	pctx := context.WithoutCancel(ctx)

	fail := func(err error) error {
		emit(Event{Type: EventError, Data: ErrorData{Message: err.Error()}})
		return err
	}

	convID, history, err := r.openConversation(pctx, in)
	if err != nil {
		return fail(err)
	}
	ts.convID = convID
	// make the turn cancellable by CancelConversation (e.g. conversation deletion)
	ctx, cancelTurn := context.WithCancel(ctx)
	defer cancelTurn()
	defer r.register(convID, cancelTurn)()
	emit(Event{Type: EventConversation, Data: ConversationData{ID: convID}})

	userMsg := llm.Message{Role: llm.RoleUser, Content: in.Text}
	if err := r.persist(pctx, convID, userMsg); err != nil {
		return fail(err)
	}
	history = append(history, userMsg)

	var defs []llm.ToolDef
	if r.opt.Tools != nil {
		for _, t := range r.opt.Tools.List() {
			defs = append(defs, llm.ToolDef{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()})
		}
	}

	tm, usage := &ts.tm, &ts.usage
	since := func() int64 { return time.Since(start).Milliseconds() }
	markSentence := func() {
		if tm.FirstSentenceMS == 0 {
			tm.FirstSentenceMS = max(since(), 1)
		}
	}

	for step := 0; step < r.opt.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		stepNo := step + 1
		ts.steps = stepNo
		msgs := make([]llm.Message, 0, len(history)+1)
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: r.opt.SystemPrompt})
		msgs = append(msgs, history...)
		stepStart := time.Now()
		var stepFirst int64 // ms from stepStart to the first visible token
		var stepUsage *llm.Usage
		var stepFinish string
		var stepCalls []llm.ToolCall
		model := r.model()
		if o := in.Observer; o != nil && o.LLMStart != nil {
			o.LLMStart(stepNo, model)
		}
		// recStep records the llm trace row (+ payload) of this step.
		recStep := func(err error, text string) {
			status := statusOf(err, ctx.Err())
			row := store.TraceRow{
				TS: stepStart, ConversationID: convID, TurnID: ts.id, Step: stepNo,
				Kind: store.TraceLLM, Name: model, Status: status,
				DurationMS: max(time.Since(stepStart).Milliseconds(), stepFirst), FirstTokenMS: stepFirst,
				FinishReason: stepFinish, Channel: ts.channel,
				Args:          fmt.Sprintf("messages=%d tools=%d", len(msgs), len(defs)),
				ResultSummary: summarizeStep(text, stepCalls),
			}
			if stepUsage != nil {
				row.PromptTokens, row.CompletionTokens = stepUsage.PromptTokens, stepUsage.CompletionTokens
			}
			if err != nil {
				row.Error = err.Error()
			}
			r.trace(row)
			if o := in.Observer; o != nil && o.LLMEnd != nil {
				o.LLMEnd(stepNo, row)
			}
			log.Debug("llm step", "step", stepNo, "model", model, "status", status,
				"duration_ms", row.DurationMS, "first_token_ms", stepFirst, "finish", stepFinish, "tool_calls", len(stepCalls))
			if r.payloadOn() {
				resp := &PayloadResponse{Text: text, ToolCalls: append([]llm.ToolCall(nil), stepCalls...), Usage: stepUsage, FinishReason: stepFinish}
				if err != nil {
					resp.Error = err.Error()
				}
				r.opt.Tracer.Payload(Payload{
					TS: stepStart, TurnID: ts.id, Step: stepNo, Kind: "llm",
					Request:  &PayloadRequest{Model: model, Temperature: r.opt.Temperature, Messages: msgs, Tools: defs},
					Response: resp,
				})
			}
		}
		ch, err := r.opt.Provider.Stream(ctx, llm.Request{
			Messages: msgs, Tools: defs, Temperature: r.opt.Temperature, Model: r.opt.Model,
		})
		if err != nil {
			recStep(err, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Error("llm stream failed", "err", err)
			return fail(err)
		}

		var (
			text     strings.Builder
			calls    []llm.ToolCall
			streamEr error
			finished bool
			sp       = NewSplitter()
			tf       TextFilter
		)
		sendSentence := func(s string) {
			if s == "" {
				return
			}
			markSentence()
			emit(Event{Type: EventSentence, Data: SentenceData{Text: s}})
		}
		pushText := func(vis string) {
			if tm.FirstTokenMS == 0 {
				tm.FirstTokenMS = max(since(), 1)
			}
			if stepFirst == 0 {
				stepFirst = max(time.Since(stepStart).Milliseconds(), 1)
			}
			text.WriteString(vis)
			emit(Event{Type: EventToken, Data: TokenData{Text: vis}})
			for _, s := range sp.Push(vis) {
				sendSentence(s)
			}
		}
	recv:
		for {
			select {
			case <-ctx.Done():
				break recv
			case ev, ok := <-ch:
				if !ok {
					break recv
				}
				switch ev.Type {
				case llm.EventDelta:
					vis := tf.Push(ev.Text)
					if vis == "" {
						continue
					}
					pushText(vis)
				case llm.EventToolCall:
					if ev.ToolCall != nil {
						calls = append(calls, *ev.ToolCall)
					}
				case llm.EventDone:
					finished = true
					stepFinish = ev.FinishReason
					ts.finish = ev.FinishReason
					if ev.Usage != nil {
						u := *ev.Usage
						stepUsage = &u
					}
					if ev.Usage != nil {
						ts.haveUsage = true
						usage.PromptTokens += ev.Usage.PromptTokens
						usage.CompletionTokens += ev.Usage.CompletionTokens
						usage.TotalTokens += ev.Usage.TotalTokens
					}
				case llm.EventError:
					streamEr = ev.Err
					if streamEr == nil {
						streamEr = errors.New("llm: unknown stream error")
					}
					break recv
				}
			}
		}

		stepCalls = calls
		if ctx.Err() != nil {
			recStep(ctx.Err(), text.String())
			// cancelled: keep what the user may already have heard
			if text.Len() > 0 {
				_ = r.persist(pctx, convID, llm.Message{Role: llm.RoleAssistant, Content: text.String()})
			}
			return ctx.Err()
		}
		if streamEr == nil && !finished {
			streamEr = errors.New("llm: stream ended without done event")
		}
		if streamEr != nil {
			recStep(streamEr, text.String())
			if text.Len() > 0 {
				_ = r.persist(pctx, convID, llm.Message{Role: llm.RoleAssistant, Content: text.String()})
			}
			log.Error("llm stream error", "err", streamEr)
			return fail(streamEr)
		}
		if tail := tf.Flush(); tail != "" {
			pushText(tail)
		}
		sendSentence(sp.Flush())

		recStep(nil, text.String())
		ts.lastText = text.String()
		asst := llm.Message{Role: llm.RoleAssistant, Content: text.String()}
		if len(calls) == 0 {
			if asst.Content != "" {
				if err := r.persist(pctx, convID, asst); err != nil {
					return fail(err)
				}
			}
			tm.TotalMS = max(since(), tm.FirstTokenMS, tm.FirstSentenceMS)
			d := DoneData{Timings: *tm}
			if ts.haveUsage {
				u := *usage
				d.Usage = &u
			}
			emit(Event{Type: EventDone, Data: d})
			return nil
		}

		// tool step
		argErrs := make([]string, len(calls))
		for i := range calls {
			if calls[i].ID == "" {
				calls[i].ID = "call_" + logx.NewID()
			}
			switch a := strings.TrimSpace(string(calls[i].Arguments)); {
			case a == "":
				calls[i].Arguments = json.RawMessage(`{}`)
			case !json.Valid([]byte(a)):
				argErrs[i] = "invalid arguments: not valid JSON"
				calls[i].Arguments = json.RawMessage(`{}`)
			}
		}
		asst.ToolCalls = calls
		if err := r.persist(pctx, convID, asst); err != nil {
			return fail(err)
		}
		history = append(history, asst)

		for i, tc := range calls {
			emit(Event{Type: EventToolCall, Data: ToolCallData{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}})
			var res json.RawMessage
			var callErr error
			toolStart := time.Now()
			switch {
			case ctx.Err() != nil:
				callErr = ctx.Err()
			case argErrs[i] != "":
				callErr = errors.New(argErrs[i])
			default:
				res, callErr = r.callTool(ctx, tc)
			}
			content := wrapResult(tc.Name, res, callErr)
			ts.tools = append(ts.tools, tc.Name)
			r.traceTool(ctx, ts, stepNo, tc, toolStart, res, callErr)
			if callErr != nil {
				log.Warn("tool error", "tool", tc.Name, "err", callErr)
			}
			tr := ToolResultData{ID: tc.ID, Name: tc.Name}
			if callErr != nil {
				tr.Error = callErr.Error()
			} else {
				tr.Result = res
			}
			toolMsg := llm.Message{Role: llm.RoleTool, Content: content, ToolCallID: tc.ID, Name: tc.Name}
			if err := r.persist(pctx, convID, toolMsg); err != nil {
				return fail(err)
			}
			history = append(history, toolMsg)
			emit(Event{Type: EventToolResult, Data: tr})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fail(fmt.Errorf("%w (%d)", ErrMaxSteps, r.opt.MaxSteps))
}

// callTool runs one tool with a per-call timeout, recovering panics, and
// returns the JSON-encoded result.
func (r *Runtime) callTool(ctx context.Context, tc llm.ToolCall) (res json.RawMessage, err error) {
	if r.opt.Tools == nil {
		return nil, fmt.Errorf("unknown tool %q", tc.Name)
	}
	t, ok := r.opt.Tools.Get(tc.Name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", tc.Name)
	}
	tctx, cancel := context.WithTimeout(ctx, r.opt.ToolTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("tool %q panicked: %v", tc.Name, p)
		}
	}()
	v, err := t.Call(tctx, tc.Arguments)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode tool result: %w", err)
	}
	return b, nil
}

// wrapResult builds the tool message content: {"tool":name,"result":...} or
// {"tool":name,"error":msg}.
func wrapResult(name string, res json.RawMessage, err error) string {
	if err != nil {
		b, _ := json.Marshal(struct {
			Tool  string `json:"tool"`
			Error string `json:"error"`
		}{name, err.Error()})
		return string(b)
	}
	if len(res) == 0 {
		res = json.RawMessage("null")
	}
	b, _ := json.Marshal(struct {
		Tool   string          `json:"tool"`
		Result json.RawMessage `json:"result"`
	}{name, res})
	return string(b)
}

// openConversation resolves/creates the conversation and loads its history.
func (r *Runtime) openConversation(ctx context.Context, in Input) (string, []llm.Message, error) {
	channel := in.Channel
	if channel == "" {
		channel = store.ChannelCLI
	}
	if r.opt.Store == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		id := in.ConversationID
		if id == "" {
			id = logx.NewID()
		}
		h := append([]llm.Message(nil), r.mem[id]...)
		return id, h, nil
	}
	s := r.opt.Store
	if in.ConversationID == "" {
		c, err := s.CreateConversation(ctx, titleOf(in.Text), channel)
		if err != nil {
			return "", nil, err
		}
		return c.ID, nil, nil
	}
	if _, err := s.GetConversation(ctx, in.ConversationID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil, fmt.Errorf("conversation %q not found", in.ConversationID)
		}
		return "", nil, err
	}
	rows, err := s.ListMessages(ctx, in.ConversationID)
	if err != nil {
		return "", nil, err
	}
	h := make([]llm.Message, 0, len(rows))
	for _, m := range rows {
		lm := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		if len(m.ToolCalls) > 0 {
			if err := json.Unmarshal(m.ToolCalls, &lm.ToolCalls); err != nil {
				return "", nil, fmt.Errorf("decode tool_calls of message %d: %w", m.ID, err)
			}
		}
		h = append(h, lm)
	}
	return in.ConversationID, h, nil
}

func (r *Runtime) persist(ctx context.Context, convID string, m llm.Message) error {
	if r.opt.Store == nil {
		r.mu.Lock()
		r.mem[convID] = append(r.mem[convID], m)
		r.mu.Unlock()
		return nil
	}
	sm := store.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
	if len(m.ToolCalls) > 0 {
		b, err := json.Marshal(m.ToolCalls)
		if err != nil {
			return err
		}
		sm.ToolCalls = b
	}
	_, err := r.opt.Store.AppendMessage(ctx, convID, sm)
	return err
}

func titleOf(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	rs := []rune(s)
	if len(rs) > 30 {
		rs = rs[:30]
	}
	return string(rs)
}
