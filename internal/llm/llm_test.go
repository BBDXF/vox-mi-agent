package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const textStream = `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"index":0,"delta":{"reasoning_content":"thinking..."}}]}

data: {"choices":[{"index":0,"delta":{"content":"你好，"}}]}

data: {"choices":[{"index":0,"delta":{"content":"world"}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}

data: [DONE]

`

const toolStream = `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search_audio","arguments":""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"set_volume","arguments":"{\"level"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"周杰伦\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\":30}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}

data: [DONE]

`

func sseServer(t *testing.T, body string, onReq func(r *http.Request, b []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if onReq != nil {
			onReq(r, b)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
}

func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var evs []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-timeout:
			t.Fatal("timeout collecting events")
		}
	}
}

func TestBuildBodyExtraBody(t *testing.T) {
	extra := map[string]any{
		"enable_thinking": false, "top_k": 20, "nested": map[string]any{"a": 1},
		"model": "evil", "messages": "evil", "tools": "evil", "stream": false, "stream_options": "evil",
	}
	b, err := buildBody("m", Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, extra)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["enable_thinking"] != false || got["top_k"] != float64(20) || got["nested"] == nil {
		t.Fatalf("extra not merged: %s", b)
	}
	so, _ := got["stream_options"].(map[string]any)
	msgs, _ := got["messages"].([]any)
	if got["model"] != "m" || got["stream"] != true || so["include_usage"] != true || len(msgs) != 1 || got["tools"] != nil {
		t.Fatalf("reserved keys overridden: %s", b)
	}
	b2, _ := buildBody("m", Request{}, nil)
	if bytes.Contains(b2, []byte("enable_thinking")) {
		t.Fatalf("unexpected: %s", b2)
	}
}

func TestOpenAITextStream(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := sseServer(t, textStream, func(r *http.Request, b []byte) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		json.Unmarshal(b, &gotBody)
	})
	defer srv.Close()
	p := NewOpenAI(Config{BaseURL: srv.URL + "/v1/", APIKey: "k", Model: "m1"})
	temp := 0.2
	ch, err := p.Stream(context.Background(), Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Tools:       []ToolDef{{Name: "t", Description: "d"}},
		Temperature: &temp,
	})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer k" {
		t.Fatalf("path=%q auth=%q", gotPath, gotAuth)
	}
	if gotBody["stream"] != true || gotBody["model"] != "m1" {
		t.Fatalf("body: %v", gotBody)
	}
	so, _ := gotBody["stream_options"].(map[string]any)
	if so["include_usage"] != true {
		t.Fatalf("stream_options: %v", gotBody["stream_options"])
	}
	if len(evs) != 3 || evs[0].Text != "你好，" || evs[1].Text != "world" {
		t.Fatalf("events: %+v", evs)
	}
	d := evs[2]
	if d.Type != EventDone || d.FinishReason != "stop" || d.Usage == nil || d.Usage.TotalTokens != 13 {
		t.Fatalf("done: %+v", d)
	}
}

func TestOpenAIToolCallStream(t *testing.T) {
	srv := sseServer(t, toolStream, nil)
	defer srv.Close()
	p := NewOpenAI(Config{BaseURL: srv.URL, Model: "m"})
	msg, err := Complete(context.Background(), p, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("calls: %+v", msg.ToolCalls)
	}
	a, b := msg.ToolCalls[0], msg.ToolCalls[1]
	if a.ID != "call_a" || a.Name != "search_audio" || string(a.Arguments) != `{"q":"周杰伦"}` {
		t.Fatalf("a=%+v args=%s", a, a.Arguments)
	}
	if b.ID != "call_b" || b.Name != "set_volume" || string(b.Arguments) != `{"level":30}` {
		t.Fatalf("b=%+v args=%s", b, b.Arguments)
	}

	// event ordering: tool calls before done
	ch, _ := p.Stream(context.Background(), Request{})
	evs := collect(t, ch)
	if len(evs) != 3 || evs[0].Type != EventToolCall || evs[1].Type != EventToolCall ||
		evs[2].Type != EventDone || evs[2].FinishReason != "tool_calls" {
		t.Fatalf("events: %+v", evs)
	}
}

func TestOpenAIRequestEncodesToolMessages(t *testing.T) {
	var got struct {
		Messages []map[string]any `json:"messages"`
		Tools    []map[string]any `json:"tools"`
	}
	srv := sseServer(t, textStream, func(_ *http.Request, b []byte) { json.Unmarshal(b, &got) })
	defer srv.Close()
	p := NewOpenAI(Config{BaseURL: srv.URL, Model: "m"})
	_, err := Complete(context.Background(), p, Request{
		Messages: []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "f", Arguments: json.RawMessage(`{"a":1}`)}}},
			{Role: RoleTool, ToolCallID: "c1", Name: "f", Content: "ok"},
		},
		Tools: []ToolDef{{Name: "f", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tcs := got.Messages[0]["tool_calls"].([]any)
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if fn["arguments"] != `{"a":1}` || fn["name"] != "f" {
		t.Fatalf("fn: %v", fn)
	}
	if _, has := got.Messages[0]["content"]; has {
		t.Fatalf("assistant tool-call msg should omit content: %v", got.Messages[0])
	}
	if got.Messages[1]["tool_call_id"] != "c1" {
		t.Fatalf("tool msg: %v", got.Messages[1])
	}
	if got.Tools[0]["type"] != "function" {
		t.Fatalf("tools: %v", got.Tools)
	}
}

func TestOpenAIErrorStatus(t *testing.T) {
	big := strings.Repeat("x", 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, big)
	}))
	defer srv.Close()
	p := NewOpenAI(Config{BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), Request{})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err=%v", err)
	}
	if len(err.Error()) > 3000 || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("not truncated: %d", len(err.Error()))
	}
}

func TestOpenAIMidStreamError(t *testing.T) {
	srv := sseServer(t, "data: {\"error\":{\"message\":\"boom\"}}\n\n", nil)
	defer srv.Close()
	_, err := Complete(context.Background(), NewOpenAI(Config{BaseURL: srv.URL}), Request{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenAITruncatedStream(t *testing.T) {
	srv := sseServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n", nil)
	defer srv.Close()
	_, err := Complete(context.Background(), NewOpenAI(Config{BaseURL: srv.URL}), Request{})
	if err == nil {
		t.Fatal("expected error for stream ending without finish")
	}
}

func TestOpenAIContextCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	p := NewOpenAI(Config{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, Request{})
	if err != nil {
		t.Fatal(err)
	}
	ev := <-ch
	if ev.Type != EventDelta || ev.Text != "hi" {
		t.Fatalf("first: %+v", ev)
	}
	cancel()
	evs := collect(t, ch)
	if len(evs) == 0 || evs[len(evs)-1].Type != EventError || !errors.Is(evs[len(evs)-1].Err, context.Canceled) {
		t.Fatalf("events: %+v", evs)
	}
}

func TestOpenAITimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	p := NewOpenAI(Config{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	_, err := p.Stream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestFakeLLM(t *testing.T) {
	tc := ToolCall{ID: "1", Name: "play", Arguments: json.RawMessage(`{}`)}
	f := NewFake(
		FakeTurn{Text: "你好世界，朋友们！", ToolCalls: []ToolCall{tc}},
		FakeTurn{Text: "partial", Err: errors.New("bad")},
	)
	ch, err := f.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	var text string
	nd := 0
	for _, e := range evs {
		if e.Type == EventDelta {
			text += e.Text
			nd++
		}
	}
	if text != "你好世界，朋友们！" || nd < 2 {
		t.Fatalf("text=%q deltas=%d", text, nd)
	}
	n := len(evs)
	if evs[n-2].Type != EventToolCall || evs[n-2].ToolCall.Name != "play" ||
		evs[n-1].Type != EventDone || evs[n-1].FinishReason != "tool_calls" {
		t.Fatalf("tail: %+v", evs[n-2:])
	}

	msg, err := Complete(context.Background(), f, Request{})
	if err == nil || err.Error() != "bad" || msg.Content != "partial" {
		t.Fatalf("msg=%+v err=%v", msg, err)
	}

	if _, err := f.Stream(context.Background(), Request{}); err == nil {
		t.Fatal("expected exhausted error")
	}
	reqs := f.Requests()
	if len(reqs) != 3 || f.Calls() != 3 || reqs[0].Messages[0].Content != "q" {
		t.Fatalf("reqs: %+v", reqs)
	}
}

func TestFakeCancel(t *testing.T) {
	f := NewFake(FakeTurn{Text: strings.Repeat("a", 1000)})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := f.Stream(ctx, Request{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("channel not closed after cancel")
		}
	}
}
