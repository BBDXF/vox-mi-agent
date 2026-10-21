package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const maxErrBody = 2048

// Config configures the OpenAI-compatible client.
type Config struct {
	BaseURL    string        // e.g. https://api.openai.com/v1 (no trailing /chat/completions)
	APIKey     string        // sent as Bearer token when non-empty
	Model      string        // default model
	Timeout    time.Duration // overall deadline per Stream call; 0 = none
	HTTPClient *http.Client  // optional; defaults to a client without timeout
}

// OpenAI is a Provider for OpenAI-compatible /chat/completions endpoints.
type OpenAI struct {
	cfg Config
	hc  *http.Client
}

// NewOpenAI creates a client.
func NewOpenAI(cfg Config) *OpenAI {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	return &OpenAI{cfg: cfg, hc: hc}
}

// Name implements Provider.
func (o *OpenAI) Name() string { return "openai" }

// wire types

type wireToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function wireToolCallFn `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type wireToolFn struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireTool struct {
	Type     string     `json:"type"`
	Function wireToolFn `json:"function"`
}

type wireRequest struct {
	Model         string        `json:"model"`
	Messages      []wireMessage `json:"messages"`
	Tools         []wireTool    `json:"tools,omitempty"`
	Temperature   *float64      `json:"temperature,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

func buildBody(model string, req Request) ([]byte, error) {
	wr := wireRequest{Model: model, Temperature: req.Temperature, Stream: true}
	wr.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{true}
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, ToolCallID: m.ToolCallID, Name: m.Name}
		if m.Content != "" || len(m.ToolCalls) == 0 {
			c := m.Content
			wm.Content = &c
		}
		for _, tc := range m.ToolCalls {
			args := string(tc.Arguments)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
				ID: tc.ID, Type: "function",
				Function: wireToolCallFn{Name: tc.Name, Arguments: args},
			})
		}
		wr.Messages = append(wr.Messages, wm)
	}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(bytes.TrimSpace(params)) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		wr.Tools = append(wr.Tools, wireTool{Type: "function",
			Function: wireToolFn{Name: t.Name, Description: t.Description, Parameters: params}})
	}
	return json.Marshal(wr)
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage          `json:"usage"`
	Error json.RawMessage `json:"error"`
}

type acc struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

// Stream implements Provider.
func (o *OpenAI) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	model := req.Model
	if model == "" {
		model = o.cfg.Model
	}
	body, err := buildBody(model, req)
	if err != nil {
		return nil, fmt.Errorf("llm: encode request: %w", err)
	}
	var cancel context.CancelFunc
	if o.cfg.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if o.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.cfg.APIKey)
	}
	resp, err := o.hc.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer cancel()
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody+1))
		s := strings.TrimSpace(string(b))
		if len(b) > maxErrBody {
			s = strings.TrimSpace(string(b[:maxErrBody])) + "...(truncated)"
		}
		return nil, fmt.Errorf("llm: http %d: %s", resp.StatusCode, s)
	}

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		defer cancel()
		defer resp.Body.Close()
		send := func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		fail := func(err error) {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			// best effort; consumer may be gone
			select {
			case out <- Event{Type: EventError, Err: err}:
			default:
				select {
				case out <- Event{Type: EventError, Err: err}:
				case <-time.After(time.Second):
				}
			}
		}
		o.readStream(ctx, resp.Body, send, fail)
	}()
	return out, nil
}

func (o *OpenAI) readStream(ctx context.Context, r io.Reader, send func(Event) bool, fail func(error)) {
	br := bufio.NewReaderSize(r, 64*1024)
	var calls []*acc
	var usage *Usage
	finish := ""
	sawDone := false

	flushCalls := func() bool {
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].index < calls[j].index })
		for _, c := range calls {
			args := c.args.String()
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			tc := &ToolCall{ID: c.id, Name: c.name, Arguments: json.RawMessage(args)}
			if !send(Event{Type: EventToolCall, ToolCall: tc}) {
				return false
			}
		}
		calls = nil
		return true
	}

	for !sawDone {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				sawDone = true
				break
			}
			if data != "" {
				var ch streamChunk
				if jerr := json.Unmarshal([]byte(data), &ch); jerr != nil {
					fail(fmt.Errorf("llm: bad stream chunk: %w", jerr))
					return
				}
				if len(ch.Error) > 0 && string(ch.Error) != "null" {
					fail(fmt.Errorf("llm: stream error: %s", truncate(string(ch.Error), maxErrBody)))
					return
				}
				if ch.Usage != nil {
					usage = ch.Usage
				}
				for _, c := range ch.Choices {
					if c.Delta.Content != "" {
						if !send(Event{Type: EventDelta, Text: c.Delta.Content}) {
							fail(ctx.Err())
							return
						}
					}
					for _, d := range c.Delta.ToolCalls {
						var a *acc
						if d.Index != nil {
							for _, x := range calls {
								if x.index == *d.Index {
									a = x
									break
								}
							}
							if a == nil {
								a = &acc{index: *d.Index}
								calls = append(calls, a)
							}
						} else if d.ID != "" || len(calls) == 0 {
							a = &acc{index: len(calls)}
							calls = append(calls, a)
						} else {
							a = calls[len(calls)-1]
						}
						if d.ID != "" {
							a.id = d.ID
						}
						if d.Function.Name != "" {
							a.name += d.Function.Name
						}
						a.args.WriteString(d.Function.Arguments)
					}
					if c.FinishReason != nil && *c.FinishReason != "" {
						finish = *c.FinishReason
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if finish != "" {
					break // tolerate missing [DONE]
				}
				fail(errors.New("llm: stream ended unexpectedly"))
				return
			}
			fail(fmt.Errorf("llm: read stream: %w", err))
			return
		}
	}
	if ctx.Err() != nil {
		fail(ctx.Err())
		return
	}
	if !flushCalls() {
		fail(ctx.Err())
		return
	}
	if finish == "" {
		finish = "stop"
	}
	send(Event{Type: EventDone, Usage: usage, FinishReason: finish})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
