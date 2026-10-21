package llm

import (
	"context"
	"errors"
	"sync"
)

// FakeTurn scripts one Stream call: Text is streamed in small chunks, then
// ToolCalls, then "done". If Err is non-nil, an error event is emitted after
// the text instead of tool calls/done.
type FakeTurn struct {
	Text      string
	ToolCalls []ToolCall
	Err       error
}

// FakeLLM is a scripted Provider for tests.
type FakeLLM struct {
	mu       sync.Mutex
	turns    []FakeTurn
	next     int
	requests []Request
	// ChunkRunes is the number of runes per delta (default 4).
	ChunkRunes int
}

// NewFake creates a FakeLLM with the given scripted turns.
func NewFake(turns ...FakeTurn) *FakeLLM {
	return &FakeLLM{turns: turns, ChunkRunes: 4}
}

// Name implements Provider.
func (f *FakeLLM) Name() string { return "fake" }

// Requests returns a copy of all requests received so far.
func (f *FakeLLM) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Calls returns the number of Stream calls received.
func (f *FakeLLM) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// Stream implements Provider. It returns an error if the script is exhausted.
func (f *FakeLLM) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	f.mu.Lock()
	// snapshot messages so later mutation by the caller doesn't affect records
	req.Messages = append([]Message(nil), req.Messages...)
	f.requests = append(f.requests, req)
	if f.next >= len(f.turns) {
		f.mu.Unlock()
		return nil, errors.New("llm fake: no scripted turns left")
	}
	turn := f.turns[f.next]
	f.next++
	n := f.ChunkRunes
	f.mu.Unlock()
	if n <= 0 {
		n = 4
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		send := func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		rs := []rune(turn.Text)
		for i := 0; i < len(rs); i += n {
			end := min(i+n, len(rs))
			if !send(Event{Type: EventDelta, Text: string(rs[i:end])}) {
				return
			}
		}
		if turn.Err != nil {
			send(Event{Type: EventError, Err: turn.Err})
			return
		}
		for i := range turn.ToolCalls {
			tc := turn.ToolCalls[i]
			if !send(Event{Type: EventToolCall, ToolCall: &tc}) {
				return
			}
		}
		fr := "stop"
		if len(turn.ToolCalls) > 0 {
			fr = "tool_calls"
		}
		send(Event{Type: EventDone, FinishReason: fr})
	}()
	return out, nil
}
