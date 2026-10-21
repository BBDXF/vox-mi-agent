package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/tools"
)

type memTracer struct {
	mu       sync.Mutex
	rows     []store.TraceRow
	payloads []Payload
	payload  bool
}

func (m *memTracer) Trace(r store.TraceRow) { m.mu.Lock(); m.rows = append(m.rows, r); m.mu.Unlock() }
func (m *memTracer) PayloadEnabled() bool   { return m.payload }
func (m *memTracer) Payload(p Payload) {
	m.mu.Lock()
	m.payloads = append(m.payloads, p)
	m.mu.Unlock()
}

func TestTraceRowsForToolTurn(t *testing.T) {
	f := llm.NewFake(
		llm.FakeTurn{Text: "好的。", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"x":1}`)}}},
		llm.FakeTurn{Text: "完成了。"},
	)
	reg := tools.NewRegistry()
	reg.Register(echoTool())
	tr := &memTracer{payload: true}
	rt := New(Options{Provider: f, Tools: reg, Model: "fake-model", Tracer: tr})
	var c collector
	if err := rt.Run(context.Background(), Input{Text: "做点事", Channel: store.ChannelWeb}, c.emit); err != nil {
		t.Fatal(err)
	}
	// emission order: llm(1), tool(1), llm(2), turn(0)
	type exp struct {
		kind string
		step int
		name string
	}
	want := []exp{{"llm", 1, "fake-model"}, {"tool", 1, "echo"}, {"llm", 2, "fake-model"}, {"turn", 0, "turn"}}
	if len(tr.rows) != len(want) {
		t.Fatalf("rows %d: %+v", len(tr.rows), tr.rows)
	}
	turnID := tr.rows[0].TurnID
	for i, w := range want {
		r := tr.rows[i]
		if r.Kind != w.kind || r.Step != w.step || r.Name != w.name || r.Status != "ok" || r.TurnID != turnID || r.Channel != "web" || r.ConversationID == "" {
			t.Errorf("row %d = %+v, want %+v", i, r, w)
		}
	}
	if tr.rows[0].FinishReason != "tool_calls" || tr.rows[2].FinishReason != "stop" || tr.rows[0].FirstTokenMS <= 0 {
		t.Errorf("llm rows %+v / %+v", tr.rows[0], tr.rows[2])
	}
	if !strings.Contains(tr.rows[1].Args, `"x":1`) || !strings.Contains(tr.rows[1].ResultSummary, `"ok":true`) {
		t.Errorf("tool row %+v", tr.rows[1])
	}
	if !strings.Contains(tr.rows[0].ResultSummary, "tool_calls: echo") {
		t.Errorf("llm summary %q", tr.rows[0].ResultSummary)
	}
	turn := tr.rows[3]
	if turn.Args != "做点事" || turn.ResultSummary != "完成了。" || turn.DurationMS < turn.FirstTokenMS || turn.FirstTokenMS <= 0 {
		t.Errorf("turn row %+v", turn)
	}
	// payloads: llm(1), tool(1), llm(2) with full request + response
	if len(tr.payloads) != 3 || tr.payloads[0].Kind != "llm" || tr.payloads[1].Kind != "tool" || tr.payloads[2].Kind != "llm" {
		t.Fatalf("payloads %+v", tr.payloads)
	}
	p0 := tr.payloads[0]
	if p0.Request.Model != "fake-model" || len(p0.Request.Messages) < 2 || len(p0.Request.Tools) != 1 ||
		p0.Response.Text != "好的。" || len(p0.Response.ToolCalls) != 1 || p0.TurnID != turnID || p0.Step != 1 {
		t.Errorf("llm payload %+v", p0)
	}
	if tp := tr.payloads[1]; tp.Tool != "echo" || !strings.Contains(string(tp.Result), `"ok":true`) || string(tp.Args) != `{"x":1}` {
		t.Errorf("tool payload %+v", tp)
	}
	// second request must carry the tool result
	last := tr.payloads[2].Request.Messages
	if last[len(last)-1].Role != llm.RoleTool {
		t.Errorf("step-2 request tail %+v", last[len(last)-1])
	}
}

func TestTracePayloadSkippedWhenDisabled(t *testing.T) {
	tr := &memTracer{}
	rt := New(Options{Provider: llm.NewFake(llm.FakeTurn{Text: "hi"}), Tracer: tr})
	if err := rt.Run(context.Background(), Input{Text: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(tr.payloads) != 0 || len(tr.rows) != 2 {
		t.Fatalf("payloads %d rows %d", len(tr.payloads), len(tr.rows))
	}
}

func TestTraceErrorsAndToolError(t *testing.T) {
	// tool error -> tool row status error, turn still ok
	f := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "boom", Arguments: json.RawMessage(`{}`)}}},
		llm.FakeTurn{Text: "出错了。"},
	)
	reg := tools.NewRegistry()
	reg.Register(fakeTool{"boom", func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("kaput") }})
	tr := &memTracer{}
	rt := New(Options{Provider: f, Tools: reg, Tracer: tr})
	if err := rt.Run(context.Background(), Input{Text: "x"}, nil); err != nil {
		t.Fatal(err)
	}
	if r := tr.rows[1]; r.Kind != "tool" || r.Status != "error" || r.Error != "kaput" {
		t.Errorf("tool row %+v", r)
	}
	if r := tr.rows[len(tr.rows)-1]; r.Kind != "turn" || r.Status != "ok" {
		t.Errorf("turn row %+v", r)
	}

	// LLM stream error -> llm row + turn row both error
	tr = &memTracer{}
	rt = New(Options{Provider: llm.NewFake(llm.FakeTurn{Text: "半", Err: errors.New("upstream 500")}), Tracer: tr})
	if err := rt.Run(context.Background(), Input{Text: "x"}, nil); err == nil {
		t.Fatal("want error")
	}
	if len(tr.rows) != 2 || tr.rows[0].Kind != "llm" || tr.rows[0].Status != "error" || !strings.Contains(tr.rows[0].Error, "upstream 500") ||
		tr.rows[1].Kind != "turn" || tr.rows[1].Status != "error" {
		t.Fatalf("rows %+v", tr.rows)
	}

	// cancellation -> canceled
	tr = &memTracer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt = New(Options{Provider: llm.NewFake(llm.FakeTurn{Text: "x"}), Tracer: tr})
	_ = rt.Run(ctx, Input{Text: "x"}, nil)
	if r := tr.rows[len(tr.rows)-1]; r.Kind != "turn" || r.Status != "canceled" {
		t.Fatalf("rows %+v", tr.rows)
	}
}
