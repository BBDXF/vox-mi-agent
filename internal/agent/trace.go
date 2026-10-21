package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/store"
)

// Tracer receives structured trace records from the runtime. Implementations
// must be non-blocking (the runtime calls them inline) and safe for
// concurrent use. A nil Tracer disables tracing.
type Tracer interface {
	// Trace records one traces-table row (turn / llm / tool).
	Trace(store.TraceRow)
	// PayloadEnabled reports whether full payloads are currently wanted, so the
	// runtime can skip building them.
	PayloadEnabled() bool
	// Payload records one full request/response (llm) or tool call (tool).
	Payload(Payload)
}

// Payload is one line of the debug payload log.
type Payload struct {
	TS     time.Time `json:"ts"`
	TurnID string    `json:"turn_id"`
	Step   int       `json:"step"`
	Kind   string    `json:"kind"` // llm | tool

	// kind=llm
	Request  *PayloadRequest  `json:"request,omitempty"`
	Response *PayloadResponse `json:"response,omitempty"`

	// kind=tool
	Tool   string          `json:"tool,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// PayloadRequest is the full LLM request (no credentials).
type PayloadRequest struct {
	Model       string        `json:"model"`
	Temperature *float64      `json:"temperature,omitempty"`
	Messages    []llm.Message `json:"messages"`
	Tools       []llm.ToolDef `json:"tools,omitempty"`
}

// PayloadResponse is the assembled LLM answer of one step.
type PayloadResponse struct {
	Text         string         `json:"text"`
	ToolCalls    []llm.ToolCall `json:"tool_calls,omitempty"`
	Usage        *llm.Usage     `json:"usage,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// turnState accumulates the per-turn facts needed for the turn trace row and
// the summary log line.
type turnState struct {
	id        string
	channel   string
	userText  string
	convID    string
	start     time.Time
	tm        Timings
	usage     llm.Usage
	haveUsage bool
	steps     int
	tools     []string
	finish    string
	lastText  string
}

func (r *Runtime) model() string {
	if r.opt.Model != "" {
		return r.opt.Model
	}
	if m, ok := r.opt.Provider.(interface{ Model() string }); ok {
		return m.Model()
	}
	return ""
}

func (r *Runtime) trace(row store.TraceRow) {
	if r.opt.Tracer != nil {
		r.opt.Tracer.Trace(row)
	}
}

func (r *Runtime) payloadOn() bool { return r.opt.Tracer != nil && r.opt.Tracer.PayloadEnabled() }

func statusOf(err error, ctxErr error) string {
	switch {
	case err == nil:
		return store.TraceOK
	case ctxErr != nil:
		return store.TraceCanceled
	}
	return store.TraceError
}

// finishTurn writes the turn trace row and the info-level summary line.
func (r *Runtime) finishTurn(ctx interface{ Err() error }, ts *turnState, err error) {
	total := max(time.Since(ts.start).Milliseconds(), ts.tm.FirstTokenMS, ts.tm.FirstSentenceMS)
	status := statusOf(err, ctx.Err())
	row := store.TraceRow{
		TS: ts.start, ConversationID: ts.convID, TurnID: ts.id, Step: 0,
		Kind: store.TraceTurn, Name: "turn", Status: status,
		DurationMS: total, FirstTokenMS: ts.tm.FirstTokenMS,
		PromptTokens: ts.usage.PromptTokens, CompletionTokens: ts.usage.CompletionTokens,
		FinishReason: ts.finish, Args: ts.userText, ResultSummary: ts.lastText, Channel: ts.channel,
	}
	if err != nil {
		row.Error = err.Error()
	}
	r.trace(row)
	attrs := []any{
		"turn_id", ts.id, "conversation", ts.convID, "status", status, "steps", ts.steps,
		"tools", len(ts.tools), "total_ms", total, "first_token_ms", ts.tm.FirstTokenMS,
		"first_sentence_ms", ts.tm.FirstSentenceMS,
		"prompt_tokens", ts.usage.PromptTokens, "completion_tokens", ts.usage.CompletionTokens,
	}
	if err != nil {
		attrs = append(attrs, "err", err.Error())
	}
	r.log.Info("turn", attrs...)
}

// summarizeStep is the one-line result summary of an llm step.
func summarizeStep(text string, calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return text
	}
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	out := "tool_calls: " + strings.Join(names, ", ")
	if text != "" {
		out = text + " | " + out
	}
	return out
}

// traceTool records the trace row (+ payload) of one tool call.
func (r *Runtime) traceTool(ctx context.Context, ts *turnState, step int, tc llm.ToolCall, start time.Time, res json.RawMessage, callErr error) {
	status := statusOf(callErr, ctx.Err())
	row := store.TraceRow{
		TS: start, ConversationID: ts.convID, TurnID: ts.id, Step: step,
		Kind: store.TraceTool, Name: tc.Name, Status: status,
		DurationMS: time.Since(start).Milliseconds(), Args: string(tc.Arguments),
		ResultSummary: string(res), Channel: ts.channel,
	}
	if callErr != nil {
		row.Error = callErr.Error()
	}
	r.trace(row)
	r.log.Debug("tool call", "turn_id", ts.id, "step", step, "tool", tc.Name, "status", status,
		"duration_ms", row.DurationMS, "err", row.Error)
	if r.payloadOn() {
		p := Payload{TS: start, TurnID: ts.id, Step: step, Kind: "tool", Tool: tc.Name, Args: tc.Arguments, Result: res}
		if callErr != nil {
			p.Error = callErr.Error()
		}
		r.opt.Tracer.Payload(p)
	}
}
