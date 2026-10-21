// Package llm defines a minimal provider abstraction for chat LLMs plus an
// OpenAI-compatible streaming client and a scripted fake for tests.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role=tool
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a complete (fully accumulated) tool invocation request.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"` // JSON text of the arguments object
}

// ToolDef describes a callable tool; Parameters is a JSON Schema object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Request is a single chat completion request.
type Request struct {
	Messages    []Message
	Tools       []ToolDef
	Temperature *float64
	Model       string // overrides the provider default when non-empty
}

// Usage reports token accounting, when the backend provides it.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// EventType enumerates stream event kinds.
type EventType string

const (
	EventDelta    EventType = "delta"
	EventToolCall EventType = "tool_call"
	EventDone     EventType = "done"
	EventError    EventType = "error"
)

// Event is one item of a streamed response.
type Event struct {
	Type         EventType
	Text         string    // delta
	ToolCall     *ToolCall // tool_call (complete)
	Usage        *Usage    // done (may be nil)
	FinishReason string    // done
	Err          error     // error
}

// Provider streams chat completions. The returned channel is closed after a
// "done" or "error" event. Tool calls are emitted complete, before "done".
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Complete drains Stream and returns the assembled assistant message.
func Complete(ctx context.Context, p Provider, req Request) (Message, error) {
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return Message{}, err
	}
	var sb strings.Builder
	msg := Message{Role: RoleAssistant}
	var streamErr error
	finished := false
	for ev := range ch {
		switch ev.Type {
		case EventDelta:
			sb.WriteString(ev.Text)
		case EventToolCall:
			if ev.ToolCall != nil {
				msg.ToolCalls = append(msg.ToolCalls, *ev.ToolCall)
			}
		case EventDone:
			finished = true
		case EventError:
			if ev.Err != nil {
				streamErr = ev.Err
			} else {
				streamErr = errors.New("llm: unknown stream error")
			}
		}
	}
	msg.Content = sb.String()
	if streamErr != nil {
		return msg, streamErr
	}
	if !finished {
		if err := ctx.Err(); err != nil {
			return msg, err
		}
		return msg, errors.New("llm: stream ended without done event")
	}
	return msg, nil
}
