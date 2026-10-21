package agent

import (
	"encoding/json"

	"github.com/andy/vox-mi-agent/internal/llm"
)

// Event types emitted by Runtime.Run (shared by CLI, SSE and the speaker).
const (
	EventConversation = "conversation"
	EventToken        = "token"
	EventSentence     = "sentence"
	EventToolCall     = "tool_call"
	EventToolResult   = "tool_result"
	EventDone         = "done"
	EventError        = "error"
)

// Event is one item of the agent event stream. Data is one of the *Data
// payload structs below (by value).
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// ConversationData is the payload of "conversation".
type ConversationData struct {
	ID string `json:"id"`
}

// TokenData is the payload of "token".
type TokenData struct {
	Text string `json:"text"`
}

// SentenceData is the payload of "sentence" (Markdown stripped, TTS-ready).
type SentenceData struct {
	Text string `json:"text"`
}

// ToolCallData is the payload of "tool_call".
type ToolCallData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultData is the payload of "tool_result".
type ToolResultData struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Timings are latency measurements in milliseconds since Run started.
// FirstTokenMS / FirstSentenceMS are 0 if no token / sentence was produced.
type Timings struct {
	FirstTokenMS    int64 `json:"first_token_ms"`
	FirstSentenceMS int64 `json:"first_sentence_ms"`
	TotalMS         int64 `json:"total_ms"`
}

// DoneData is the payload of "done". Usage is summed over all LLM calls and
// is nil if the provider reported none.
type DoneData struct {
	Usage   *llm.Usage `json:"usage,omitempty"`
	Timings Timings    `json:"timings"`
}

// ErrorData is the payload of "error".
type ErrorData struct {
	Message string `json:"message"`
}
