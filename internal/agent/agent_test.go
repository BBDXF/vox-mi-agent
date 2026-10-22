package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

type fakeTool struct {
	name string
	fn   func(ctx context.Context, args json.RawMessage) (any, error)
}

func (f fakeTool) Name() string            { return f.name }
func (f fakeTool) Description() string     { return "fake " + f.name }
func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) Call(ctx context.Context, a json.RawMessage) (any, error) {
	return f.fn(ctx, a)
}

type collector struct {
	mu  sync.Mutex
	evs []Event
}

func (c *collector) emit(e Event) { c.mu.Lock(); c.evs = append(c.evs, e); c.mu.Unlock() }
func (c *collector) types() []string {
	var out []string
	for _, e := range c.evs {
		out = append(out, e.Type)
	}
	return out
}
func (c *collector) find(typ string) []Event {
	var out []Event
	for _, e := range c.evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}
func (c *collector) sentences() []string {
	var out []string
	for _, e := range c.find(EventSentence) {
		out = append(out, e.Data.(SentenceData).Text)
	}
	return out
}

func echoTool() fakeTool {
	return fakeTool{"echo", func(ctx context.Context, a json.RawMessage) (any, error) {
		return map[string]any{"ok": true, "args": json.RawMessage(a)}, nil
	}}
}

func TestPlainAnswer(t *testing.T) {
	f := llm.NewFake(llm.FakeTurn{Text: "你好！我是助手。"})
	temp := 0.3
	rt := New(Options{Provider: f, Model: "m1", Temperature: &temp})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "hi"}, c.emit); err != nil {
		t.Fatal(err)
	}
	ty := c.types()
	if ty[0] != EventConversation || ty[len(ty)-1] != EventDone {
		t.Fatalf("types %v", ty)
	}
	if got := c.sentences(); len(got) != 2 || got[0] != "你好！" || got[1] != "我是助手。" {
		t.Fatalf("sentences %q", got)
	}
	var sb strings.Builder
	for _, e := range c.find(EventToken) {
		sb.WriteString(e.Data.(TokenData).Text)
	}
	if sb.String() != "你好！我是助手。" {
		t.Fatalf("tokens %q", sb.String())
	}
	d := c.find(EventDone)[0].Data.(DoneData)
	if d.Timings.FirstTokenMS <= 0 || d.Timings.FirstSentenceMS <= 0 || d.Timings.TotalMS < d.Timings.FirstSentenceMS {
		t.Fatalf("timings %+v", d.Timings)
	}
	req := f.Requests()[0]
	if req.Model != "m1" || req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("req opts %+v", req)
	}
	if req.Messages[0].Role != llm.RoleSystem || req.Messages[0].Content != DefaultSystemPrompt {
		t.Fatalf("system prompt missing")
	}
	if _, err := json.Marshal(c.evs); err != nil {
		t.Fatal(err)
	}
}

func TestThinkAndSpecialTokensFiltered(t *testing.T) {
	f := llm.NewFake(llm.FakeTurn{Text: "<think>先想想。</think>\n\n好的。</tool_call><|im_end|>"})
	rt := New(Options{Provider: f})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "hi", ConversationID: "c1"}, c.emit); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, e := range c.find(EventToken) {
		sb.WriteString(e.Data.(TokenData).Text)
	}
	if sb.String() != "好的。" {
		t.Fatalf("tokens %q", sb.String())
	}
	if got := c.sentences(); len(got) != 1 || got[0] != "好的。" {
		t.Fatalf("sentences %q", got)
	}
	h := rt.mem["c1"]
	if last := h[len(h)-1]; last.Role != llm.RoleAssistant || last.Content != "好的。" {
		t.Fatalf("persisted %+v", last)
	}
}

func TestToolRoundTrip(t *testing.T) {
	f := llm.NewFake(
		llm.FakeTurn{Text: "好的，我来查。", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"x":1}`)}}},
		llm.FakeTurn{Text: "查到了。"},
	)
	rt := New(Options{Provider: f, Tools: tools.NewRegistry(echoTool())})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "查一下"}, c.emit); err != nil {
		t.Fatal(err)
	}
	reqs := f.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests %d", len(reqs))
	}
	if len(reqs[0].Tools) != 1 || reqs[0].Tools[0].Name != "echo" || string(reqs[0].Tools[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("tool defs %+v", reqs[0].Tools)
	}
	m := reqs[1].Messages
	// system, user, assistant(tool_calls), tool
	if len(m) != 4 || m[2].Role != llm.RoleAssistant || len(m[2].ToolCalls) != 1 || m[3].Role != llm.RoleTool {
		t.Fatalf("second request messages %+v", m)
	}
	if m[3].ToolCallID != "c1" {
		t.Fatalf("tool_call_id %q", m[3].ToolCallID)
	}
	var w struct {
		Tool   string          `json:"tool"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(m[3].Content), &w); err != nil || w.Tool != "echo" || !strings.Contains(string(w.Result), `"ok":true`) {
		t.Fatalf("wrapped %q err=%v", m[3].Content, err)
	}
	ty := strings.Join(c.types(), ",")
	if !strings.Contains(ty, "tool_call,tool_result") || !strings.HasSuffix(ty, "done") {
		t.Fatalf("types %s", ty)
	}
	tr := c.find(EventToolResult)[0].Data.(ToolResultData)
	if tr.Error != "" || !strings.Contains(string(tr.Result), `"ok":true`) {
		t.Fatalf("tool_result %+v", tr)
	}
	if got := c.sentences(); len(got) != 2 || got[0] != "好的，我来查。" {
		t.Fatalf("sentences %q", got)
	}
}

func TestUnknownToolAndBadArgs(t *testing.T) {
	f := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{
			{ID: "a", Name: "nope", Arguments: json.RawMessage(`{}`)},
			{ID: "b", Name: "echo", Arguments: json.RawMessage(`{bad`)},
			{ID: "c", Name: "boom", Arguments: json.RawMessage(`{}`)},
		}},
		llm.FakeTurn{Text: "出错了。"},
	)
	boom := fakeTool{"boom", func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("kaboom") }}
	rt := New(Options{Provider: f, Tools: tools.NewRegistry(echoTool(), boom)})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "x"}, c.emit); err != nil {
		t.Fatal(err)
	}
	trs := c.find(EventToolResult)
	if len(trs) != 3 {
		t.Fatalf("results %d", len(trs))
	}
	for _, e := range trs {
		if e.Data.(ToolResultData).Error == "" {
			t.Fatalf("expected error: %+v", e)
		}
	}
	if !strings.Contains(trs[0].Data.(ToolResultData).Error, "unknown tool") ||
		!strings.Contains(trs[2].Data.(ToolResultData).Error, "kaboom") {
		t.Fatalf("errors %+v", trs)
	}
	m := f.Requests()[1].Messages
	if len(m) != 6 { // sys user asst tool*3
		t.Fatalf("messages %d", len(m))
	}
	if !strings.Contains(m[3].Content, `"tool":"nope"`) || !strings.Contains(m[3].Content, `"error"`) {
		t.Fatalf("tool msg %q", m[3].Content)
	}
	if _, err := json.Marshal(m[2].ToolCalls); err != nil {
		t.Fatalf("tool calls not marshalable: %v", err)
	}
	if last := c.evs[len(c.evs)-1]; last.Type != EventDone {
		t.Fatalf("last %s", last.Type)
	}
}

func TestMaxSteps(t *testing.T) {
	call := llm.ToolCall{ID: "c", Name: "echo", Arguments: json.RawMessage(`{}`)}
	f := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{call}},
		llm.FakeTurn{ToolCalls: []llm.ToolCall{call}},
		llm.FakeTurn{ToolCalls: []llm.ToolCall{call}},
	)
	rt := New(Options{Provider: f, Tools: tools.NewRegistry(echoTool()), MaxSteps: 2})
	var c collector
	err := rt.Run(context.Background(), Input{Text: "loop"}, c.emit)
	if !errors.Is(err, ErrMaxSteps) {
		t.Fatalf("err %v", err)
	}
	if f.Calls() != 2 {
		t.Fatalf("calls %d", f.Calls())
	}
	if last := c.evs[len(c.evs)-1]; last.Type != EventError {
		t.Fatalf("last %s", last.Type)
	}
}

func TestProviderError(t *testing.T) {
	f := llm.NewFake(llm.FakeTurn{Text: "部分", Err: errors.New("upstream 500")})
	rt := New(Options{Provider: f})
	var c collector
	err := rt.Run(context.Background(), Input{Text: "x"}, c.emit)
	if err == nil || !strings.Contains(err.Error(), "upstream 500") {
		t.Fatalf("err %v", err)
	}
	e := c.evs[len(c.evs)-1]
	if e.Type != EventError || !strings.Contains(e.Data.(ErrorData).Message, "upstream 500") {
		t.Fatalf("event %+v", e)
	}
	// also: Stream() itself failing (script exhausted)
	var c2 collector
	if err := rt.Run(context.Background(), Input{Text: "y"}, c2.emit); err == nil {
		t.Fatal("expected error")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"q":"a"}`)}}},
		llm.FakeTurn{Text: "第一轮答案。"},
		llm.FakeTurn{Text: "第二轮答案。"},
	)
	rt := New(Options{Provider: f, Tools: tools.NewRegistry(echoTool()), Store: st})
	var c1 collector
	if err := rt.Run(ctx, Input{Text: "问题一", Channel: "web"}, c1.emit); err != nil {
		t.Fatal(err)
	}
	id := c1.evs[0].Data.(ConversationData).ID
	msgs, err := st.ListMessages(ctx, id)
	if err != nil || len(msgs) != 4 {
		t.Fatalf("stored %d err=%v", len(msgs), err)
	}
	roles := []string{msgs[0].Role, msgs[1].Role, msgs[2].Role, msgs[3].Role}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant" || msgs[2].ToolCallID != "c1" || msgs[2].Name != "echo" {
		t.Fatalf("stored %+v", msgs)
	}
	conv, _ := st.GetConversation(ctx, id)
	if conv.Channel != "web" || conv.Title != "问题一" {
		t.Fatalf("conv %+v", conv)
	}

	// a fresh runtime (simulating restart) continues the history
	rt2 := New(Options{Provider: f, Tools: tools.NewRegistry(echoTool()), Store: st})
	var c2 collector
	if err := rt2.Run(ctx, Input{ConversationID: id, Text: "问题二"}, c2.emit); err != nil {
		t.Fatal(err)
	}
	m := f.Requests()[2].Messages
	// system + 4 stored + new user
	if len(m) != 6 || m[2].Role != llm.RoleAssistant || len(m[2].ToolCalls) != 1 ||
		m[2].ToolCalls[0].ID != "c1" || string(m[2].ToolCalls[0].Arguments) != `{"q":"a"}` ||
		m[3].ToolCallID != "c1" || m[5].Content != "问题二" {
		t.Fatalf("history %+v", m)
	}
	msgs, _ = st.ListMessages(ctx, id)
	if len(msgs) != 6 {
		t.Fatalf("stored after 2nd run %d", len(msgs))
	}
	var c3 collector
	if err := rt2.Run(ctx, Input{ConversationID: "missing", Text: "x"}, c3.emit); err == nil {
		t.Fatal("expected not found")
	}
}

func TestInMemoryHistory(t *testing.T) {
	f := llm.NewFake(llm.FakeTurn{Text: "一。"}, llm.FakeTurn{Text: "二。"})
	rt := New(Options{Provider: f})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "a"}, c.emit); err != nil {
		t.Fatal(err)
	}
	id := c.evs[0].Data.(ConversationData).ID
	if id == "" {
		t.Fatal("no id")
	}
	if err := rt.Run(context.Background(), Input{ConversationID: id, Text: "b"}, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(f.Requests()[1].Messages); n != 4 {
		t.Fatalf("messages %d", n)
	}
}

// blockingProvider emits one delta then blocks until ctx is cancelled.
type blockingProvider struct{ started chan struct{} }

func (blockingProvider) Name() string { return "block" }
func (b blockingProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	out := make(chan llm.Event, 1)
	out <- llm.Event{Type: llm.EventDelta, Text: "说到一半"}
	close(b.started)
	go func() {
		<-ctx.Done()
		close(out)
	}()
	return out, nil
}

func TestCancellationPersistsPartial(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	bp := blockingProvider{started: make(chan struct{})}
	rt := New(Options{Provider: bp, Store: st})
	ctx, cancel := context.WithCancel(context.Background())
	var c collector
	errc := make(chan error, 1)
	go func() { errc <- rt.Run(ctx, Input{Text: "hi"}, c.emit) }()
	<-bp.started
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly")
	}
	c.mu.Lock()
	id := c.evs[0].Data.(ConversationData).ID
	c.mu.Unlock()
	msgs, _ := st.ListMessages(context.Background(), id)
	if len(msgs) != 2 || msgs[1].Role != llm.RoleAssistant || msgs[1].Content != "说到一半" {
		t.Fatalf("stored %+v", msgs)
	}
}

func TestCancelDuringToolKeepsHistoryConsistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	slow := fakeTool{"slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	f := llm.NewFake(llm.FakeTurn{ToolCalls: []llm.ToolCall{
		{ID: "1", Name: "slow", Arguments: json.RawMessage(`{}`)},
		{ID: "2", Name: "slow", Arguments: json.RawMessage(`{}`)},
	}})
	rt := New(Options{Provider: f, Tools: tools.NewRegistry(slow)})
	var c collector
	err := rt.Run(ctx, Input{Text: "x"}, c.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if n := len(c.find(EventToolResult)); n != 2 {
		t.Fatalf("tool results %d", n)
	}
	id := c.evs[0].Data.(ConversationData).ID
	rt.mu.Lock()
	n := len(rt.mem[id])
	rt.mu.Unlock()
	if n != 4 { // user, assistant, tool, tool
		t.Fatalf("history %d", n)
	}
}

func TestTrimHistoryStartsAtUser(t *testing.T) {
	h := []llm.Message{
		{Role: llm.RoleUser, Content: "1"}, {Role: llm.RoleAssistant, Content: "a"},
		{Role: llm.RoleUser, Content: "2"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "t"}}},
		{Role: llm.RoleTool, ToolCallID: "c", Content: "r"}, {Role: llm.RoleAssistant, Content: "b"},
	}
	if got := trimHistory(h, 0); len(got) != 6 {
		t.Error("0 = keep all")
	}
	if got := trimHistory(h, 3); len(got) != 0 {
		t.Errorf("orphan tool messages must be dropped: %+v", got)
	}
	if got := trimHistory(h, 4); len(got) != 4 || got[0].Content != "2" {
		t.Errorf("%+v", got)
	}
}
