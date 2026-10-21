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

	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/logx"
	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/tools"
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
}

// Input is one user turn.
type Input struct {
	ConversationID string // empty = new conversation
	Channel        string // web|cli|speaker (default cli)
	Text           string
}

// Runtime runs agent turns. It is safe for concurrent use.
type Runtime struct {
	opt Options
	log *slog.Logger

	mu  sync.Mutex
	mem map[string][]llm.Message // used when Store == nil
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
	return &Runtime{opt: opt, log: lg, mem: map[string][]llm.Message{}}
}

// Run executes one user turn, emitting events as they occur. It returns a
// non-nil error on provider/store failure, ErrMaxSteps, or ctx cancellation.
func (r *Runtime) Run(ctx context.Context, in Input, emit func(Event)) error {
	if emit == nil {
		emit = func(Event) {}
	}
	start := time.Now()
	ctx = logx.WithTurnID(ctx, logx.NewID())
	log := r.log.With("turn_id", logx.TurnID(ctx))
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

	var (
		tm        Timings
		usage     llm.Usage
		haveUsage bool
	)
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
		msgs := make([]llm.Message, 0, len(history)+1)
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: r.opt.SystemPrompt})
		msgs = append(msgs, history...)
		ch, err := r.opt.Provider.Stream(ctx, llm.Request{
			Messages: msgs, Tools: defs, Temperature: r.opt.Temperature, Model: r.opt.Model,
		})
		if err != nil {
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
		)
		sendSentence := func(s string) {
			if s == "" {
				return
			}
			markSentence()
			emit(Event{Type: EventSentence, Data: SentenceData{Text: s}})
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
					if ev.Text == "" {
						continue
					}
					if tm.FirstTokenMS == 0 {
						tm.FirstTokenMS = max(since(), 1)
					}
					text.WriteString(ev.Text)
					emit(Event{Type: EventToken, Data: TokenData{Text: ev.Text}})
					for _, s := range sp.Push(ev.Text) {
						sendSentence(s)
					}
				case llm.EventToolCall:
					if ev.ToolCall != nil {
						calls = append(calls, *ev.ToolCall)
					}
				case llm.EventDone:
					finished = true
					if ev.Usage != nil {
						haveUsage = true
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

		if ctx.Err() != nil {
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
			if text.Len() > 0 {
				_ = r.persist(pctx, convID, llm.Message{Role: llm.RoleAssistant, Content: text.String()})
			}
			log.Error("llm stream error", "err", streamEr)
			return fail(streamEr)
		}
		sendSentence(sp.Flush())

		asst := llm.Message{Role: llm.RoleAssistant, Content: text.String()}
		if len(calls) == 0 {
			if asst.Content != "" {
				if err := r.persist(pctx, convID, asst); err != nil {
					return fail(err)
				}
			}
			tm.TotalMS = max(since(), tm.FirstTokenMS, tm.FirstSentenceMS)
			d := DoneData{Timings: tm}
			if haveUsage {
				u := usage
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
			switch {
			case ctx.Err() != nil:
				callErr = ctx.Err()
			case argErrs[i] != "":
				callErr = errors.New(argErrs[i])
			default:
				res, callErr = r.callTool(ctx, tc)
			}
			content := wrapResult(tc.Name, res, callErr)
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
