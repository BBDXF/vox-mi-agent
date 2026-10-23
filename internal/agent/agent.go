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
	"sync/atomic"
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
	// TurnTimeout bounds a whole Run (0 = unbounded).
	TurnTimeout time.Duration
	// HistoryMessages keeps only the last N stored messages as context
	// (0 = all).
	HistoryMessages int
	// IdleReset (speaker channel only) starts a fresh context after this
	// much silence (0 = off). ContextTokens is the estimated token budget
	// of a request (0 = off).
	IdleReset     time.Duration
	ContextTokens int
	// DisabledTools are tool names not offered to (nor callable by) the LLM.
	DisabledTools []string
	// PromptContext, if set, returns text appended to the system prompt on
	// every turn. enabled reports whether a tool is registered and not
	// disabled. Empty result = nothing appended.
	PromptContext func(enabled func(name string) bool) string
}

// Settings are the hot-reloadable knobs of a Runtime (see Runtime.Apply).
type Settings struct {
	SystemPrompt    string // empty = DefaultSystemPrompt
	MaxSteps        int    // <= 0 = 6
	Temperature     *float64
	TurnTimeout     time.Duration
	HistoryMessages int
	IdleReset       time.Duration
	ContextTokens   int
	DisabledTools   []string
}

type liveSettings struct {
	Settings
	disabled map[string]bool
}

// Apply replaces the live settings; running turns keep the snapshot they
// started with.
func (r *Runtime) Apply(s Settings) {
	if s.SystemPrompt == "" {
		s.SystemPrompt = DefaultSystemPrompt
	}
	if s.MaxSteps <= 0 {
		s.MaxSteps = 6
	}
	ls := &liveSettings{Settings: s, disabled: map[string]bool{}}
	ls.DisabledTools = append([]string(nil), s.DisabledTools...)
	for _, n := range s.DisabledTools {
		ls.disabled[n] = true
	}
	r.live.Store(ls)
}

func (r *Runtime) settings() *liveSettings { return r.live.Load() }

// trimHistory keeps the last n messages, starting at a user message so tool
// results are never orphaned.
func trimHistory(h []llm.Message, n int) []llm.Message {
	if n <= 0 || len(h) <= n {
		return h
	}
	h = h[len(h)-n:]
	for i, m := range h {
		if m.Role == llm.RoleUser {
			return h[i:]
		}
	}
	return nil
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
	opt  Options
	log  *slog.Logger
	live atomic.Pointer[liveSettings]

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
	r := &Runtime{opt: opt, log: lg, mem: map[string][]llm.Message{}, active: map[*activeTurn]struct{}{}}
	r.Apply(Settings{SystemPrompt: opt.SystemPrompt, MaxSteps: opt.MaxSteps, Temperature: opt.Temperature,
		TurnTimeout: opt.TurnTimeout, HistoryMessages: opt.HistoryMessages, IdleReset: opt.IdleReset, ContextTokens: opt.ContextTokens, DisabledTools: opt.DisabledTools})
	return r
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
	if d := r.settings().TurnTimeout; d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
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

	cfg := r.settings()
	convID, prior, times, err := r.openConversation(pctx, in)
	if err != nil {
		return fail(err)
	}
	if ts.channel == store.ChannelSpeaker {
		prior = applyIdle(prior, times, cfg.IdleReset, time.Now())
	}
	prior = trimHistory(prior, cfg.HistoryMessages)
	prior = truncateToolResults(prior)
	ts.convID = convID
	// make the turn cancellable by CancelConversation (e.g. conversation deletion)
	ctx, cancelTurn := context.WithCancel(ctx)
	defer cancelTurn()
	defer r.register(convID, cancelTurn)()
	emit(Event{Type: EventConversation, Data: ConversationData{ID: convID}})

	var defs []llm.ToolDef
	if r.opt.Tools != nil {
		for _, t := range r.opt.Tools.List() {
			if cfg.disabled[t.Name()] {
				continue
			}
			defs = append(defs, llm.ToolDef{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()})
		}
	}

	sysPrompt := cfg.SystemPrompt
	if r.opt.PromptContext != nil {
		enabled := func(name string) bool {
			if cfg.disabled[name] || r.opt.Tools == nil {
				return false
			}
			_, ok := r.opt.Tools.Get(name)
			return ok
		}
		if extra := strings.TrimSpace(r.opt.PromptContext(enabled)); extra != "" {
			sysPrompt += "\n\n" + extra
		}
	}

	userMsg := llm.Message{Role: llm.RoleUser, Content: in.Text}
	if cfg.ContextTokens > 0 {
		fixed := estTokens(sysPrompt) + estTokens(in.Text) + 8
		for _, d := range defs {
			fixed += estTokens(d.Name) + estTokens(d.Description) + estTokens(string(d.Parameters))
		}
		prior = fitBudget(prior, fixed, cfg.ContextTokens)
	}
	overflowTries := 0
	if err := r.persist(pctx, convID, userMsg); err != nil {
		return fail(err)
	}
	cur := []llm.Message{userMsg}

	tm, usage := &ts.tm, &ts.usage
	since := func() int64 { return time.Since(start).Milliseconds() }
	markSentence := func() {
		if tm.FirstSentenceMS == 0 {
			tm.FirstSentenceMS = max(since(), 1)
		}
	}

	for step := 0; step < cfg.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		stepNo := step + 1
		ts.steps = stepNo
		msgs := make([]llm.Message, 0, len(prior)+len(cur)+1)
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: sysPrompt})
		msgs = append(msgs, prior...)
		msgs = append(msgs, cur...)
		// Last allowed step after tool use: offer no tools so the model must
		// answer from what it has instead of failing with ErrMaxSteps.
		stepDefs := defs
		if stepNo == cfg.MaxSteps && step > 0 && len(defs) > 0 {
			stepDefs = nil
			msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: "工具调用次数已用完。请根据已有的工具结果直接回答用户；如果没能完成，简短说明原因。"})
		}
		// shrink drops history after a context-length error; false = nothing to drop.
		shrink := func(err error) bool {
			if overflowTries >= 2 || len(prior) == 0 || !isContextOverflow(err) {
				return false
			}
			overflowTries++
			before := len(prior)
			if overflowTries == 1 {
				prior = trimHistory(prior, before/2)
			} else {
				prior = nil
			}
			log.Warn("context overflow, retrying with less history", "err", err, "from", before, "to", len(prior))
			r.trace(store.TraceRow{TS: time.Now(), ConversationID: convID, TurnID: ts.id, Step: stepNo,
				Kind: store.TraceLLM, Name: r.model(), Status: store.TraceOK, Channel: ts.channel,
				Args:          fmt.Sprintf("context overflow retry %d: history %d -> %d", overflowTries, before, len(prior)),
				ResultSummary: "context_overflow_retry"})
			return true
		}
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
				Args:          fmt.Sprintf("messages=%d tools=%d", len(msgs), len(stepDefs)),
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
					Request:  &PayloadRequest{Model: model, Temperature: cfg.Temperature, Messages: msgs, Tools: stepDefs},
					Response: resp,
				})
			}
		}
		ch, err := r.opt.Provider.Stream(ctx, llm.Request{
			Messages: msgs, Tools: stepDefs, Temperature: cfg.Temperature, Model: r.opt.Model,
		})
		if err != nil {
			recStep(err, "")
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if shrink(err) {
				step--
				continue
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
			if text.Len() == 0 && len(calls) == 0 && shrink(streamEr) {
				step--
				continue
			}
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
			case !json.Valid([]byte(a)) && json.Valid([]byte(repairJSON(a))):
				// small models often leave inner quotes unescaped, e.g. jq filters
				log.InfoContext(ctx, "tool arguments repaired", "tool", calls[i].Name)
				calls[i].Arguments = json.RawMessage(repairJSON(a))
			case !json.Valid([]byte(a)):
				snip := []rune(a)
				if len(snip) > 300 {
					snip = append(snip[:300], '…')
				}
				argErrs[i] = "invalid arguments: not valid JSON (received: " + string(snip) + "). Resend the call with a single valid JSON object."
				log.WarnContext(ctx, "tool arguments not valid JSON", "tool", calls[i].Name, "raw", string(snip))
				calls[i].Arguments = json.RawMessage(`{}`)
			}
		}
		asst.ToolCalls = calls
		if err := r.persist(pctx, convID, asst); err != nil {
			return fail(err)
		}
		cur = append(cur, asst)

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
				res, callErr = r.callTool(ctx, cfg, tc)
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
			cur = append(cur, toolMsg)
			emit(Event{Type: EventToolResult, Data: tr})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fail(fmt.Errorf("%w (%d)", ErrMaxSteps, cfg.MaxSteps))
}

// callTool runs one tool with a per-call timeout, recovering panics, and
// returns the JSON-encoded result.
func (r *Runtime) callTool(ctx context.Context, cfg *liveSettings, tc llm.ToolCall) (res json.RawMessage, err error) {
	if r.opt.Tools == nil {
		return nil, fmt.Errorf("unknown tool %q", tc.Name)
	}
	name := r.resolveToolName(tc.Name)
	t, ok := r.opt.Tools.Get(name)
	if !ok || cfg.disabled[name] {
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
func (r *Runtime) openConversation(ctx context.Context, in Input) (string, []llm.Message, []time.Time, error) {
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
		return id, h, nil, nil
	}
	s := r.opt.Store
	if in.ConversationID == "" {
		c, err := s.CreateConversation(ctx, titleOf(in.Text), channel)
		if err != nil {
			return "", nil, nil, err
		}
		return c.ID, nil, nil, nil
	}
	if _, err := s.GetConversation(ctx, in.ConversationID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil, nil, fmt.Errorf("conversation %q not found", in.ConversationID)
		}
		return "", nil, nil, err
	}
	rows, err := s.ListMessages(ctx, in.ConversationID)
	if err != nil {
		return "", nil, nil, err
	}
	h := make([]llm.Message, 0, len(rows))
	times := make([]time.Time, 0, len(rows))
	for _, m := range rows {
		lm := llm.Message{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID, Name: m.Name}
		if len(m.ToolCalls) > 0 {
			if err := json.Unmarshal(m.ToolCalls, &lm.ToolCalls); err != nil {
				return "", nil, nil, fmt.Errorf("decode tool_calls of message %d: %w", m.ID, err)
			}
		}
		h = append(h, lm)
		times = append(times, m.CreatedAt)
	}
	return in.ConversationID, h, times, nil
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

// resolveToolName maps names small models commonly mangle back to a
// registered tool: a dropped "mcp__" prefix ("fetch__fetch") or just the
// bare MCP tool name ("fetch") when it is unique.
func (r *Runtime) resolveToolName(name string) string {
	if _, ok := r.opt.Tools.Get(name); ok {
		return name
	}
	if _, ok := r.opt.Tools.Get("mcp__" + name); ok {
		return "mcp__" + name
	}
	match := ""
	for _, t := range r.opt.Tools.List() {
		if n := t.Name(); strings.HasPrefix(n, "mcp__") && strings.HasSuffix(n, "__"+name) {
			if match != "" {
				return name // ambiguous
			}
			match = n
		}
	}
	if match != "" {
		return match
	}
	return name
}
