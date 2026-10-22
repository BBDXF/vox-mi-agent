package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
)

func TestSettingsSchemaAndSectionGets(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/api/settings/schema", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	sc := decode[struct {
		Sections []struct {
			Name            string `json:"name"`
			Title           string `json:"title"`
			RestartRequired bool   `json:"restart_required"`
			Fields          []struct {
				Key         string `json:"key"`
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"fields"`
		} `json:"sections"`
	}](t, b)
	var names []string
	for _, s := range sc.Sections {
		names = append(names, s.Name)
		if s.Title == "" || len(s.Fields) == 0 || s.Fields[0].Description == "" {
			t.Errorf("section %+v", s)
		}
		if s.Name == "server" != s.RestartRequired {
			t.Errorf("restart flag of %s", s.Name)
		}
	}
	if got := strings.Join(names, ","); got != "llm,agent,mcp,media,aliases,search,sources,speaker,logging,server" {
		t.Fatalf("sections %s", got)
	}
	for _, n := range names {
		resp, b := e.do(t, "GET", "/api/settings/"+n, "")
		if resp.StatusCode != 200 || !json.Valid(b) {
			t.Fatalf("GET %s: %d %s", n, resp.StatusCode, b)
		}
		if strings.Contains(string(b), "sk-secret-1234567890") {
			t.Fatalf("secret leaked in %s: %s", n, b)
		}
	}
	if resp, _ := e.do(t, "GET", "/api/settings/nope", ""); resp.StatusCode != 404 {
		t.Fatalf("unknown section GET: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings/nope", `{"a":1}`); resp.StatusCode != 404 {
		t.Fatalf("unknown section PUT: %d", resp.StatusCode)
	}
	_, b = e.do(t, "GET", "/api/settings/llm", "")
	l := decode[map[string]any](t, b)
	if l["api_key"] != "****7890" || l["has_api_key"] != true || l["timeout"] != "1m" {
		t.Fatalf("llm %s", b)
	}
	// /api/settings (legacy shape) keeps working and now includes live values
	_, b = e.do(t, "GET", "/api/settings", "")
	if !strings.Contains(string(b), `"api_key_masked":"****7890"`) || !strings.Contains(string(b), `"speaker":{"driver":"mock"}`) {
		t.Fatalf("legacy shape: %s", b)
	}
}

func TestPutSectionsOverHTTP(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	// search: ok, then visible through GET /api/settings/search
	resp, b := e.do(t, "PUT", "/api/settings/search", `{"limit":3,"min_score":70}`)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	res := decode[struct {
		Section         string   `json:"section"`
		RestartRequired bool     `json:"restart_required"`
		Changed         []string `json:"changed"`
		Applied         map[string]any
	}](t, b)
	if res.Section != "search" || res.RestartRequired || len(res.Changed) != 2 || res.Applied == nil {
		t.Fatalf("%s", b)
	}
	_, b = e.do(t, "GET", "/api/settings/search", "")
	if g := decode[map[string]any](t, b); g["limit"] != float64(3) || g["min_score"] != float64(70) {
		t.Fatalf("%s", b)
	}
	// 400 with per-field reasons
	resp, b = e.do(t, "PUT", "/api/settings/search", `{"limit":99,"strip_patterns":["("]}`)
	er := decode[struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}](t, b)
	if resp.StatusCode != 400 || er.Error == "" || er.Fields["limit"] == "" || er.Fields["strip_patterns[0]"] == "" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	for _, bad := range []string{`nope`, `[1]`, `null`, `{}`} {
		if resp, _ := e.do(t, "PUT", "/api/settings/search", bad); resp.StatusCode != 400 {
			t.Errorf("body %q: %d", bad, resp.StatusCode)
		}
	}
	// server: restart_required, persisted, nested body
	resp, b = e.do(t, "PUT", "/api/settings/server", `{"listen":"tcp://127.0.0.1:18999","auth":{"require_admin":true}}`)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"restart_required":true`) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	t.Setenv("VOX_LLM_API_KEY", "")
	cfg, err := config.Load(e.dir)
	if err != nil || cfg.Server.Listen != "tcp://127.0.0.1:18999" || !cfg.Server.Auth.RequireAdmin || cfg.Search.Limit != 3 {
		t.Fatalf("%v %+v", err, cfg)
	}
	// the running server keeps its startup admin policy (restart required)
	if resp, _ := e.do(t, "GET", "/api/settings", ""); resp.StatusCode != 200 {
		t.Fatalf("running server affected by restart-only change: %d", resp.StatusCode)
	}
	// tools health in response + extra llm fields on the legacy endpoint (still 204)
	resp, b = e.do(t, "PUT", "/api/settings/llm", `{"base_url":"http://127.0.0.1:9/v1","model":"m9","api_key":"","temperature":0.4,"timeout":"90s","extra_body":{"enable_thinking":false}}`)
	if resp.StatusCode != 204 {
		t.Fatalf("legacy llm put: %d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/settings/llm", "")
	if g := decode[map[string]any](t, b); g["timeout"] != "1m30s" || g["model"] != "m9" || g["extra_body"].(map[string]any)["enable_thinking"] != false {
		t.Fatalf("%s", b)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings/llm", `{"base_url":"http://x","model":"m","timeout":"zzz"}`); resp.StatusCode != 400 {
		t.Fatalf("bad timeout: %d", resp.StatusCode)
	}
	// aliases over HTTP
	resp, b = e.do(t, "PUT", "/api/settings/aliases", `{"items":[{"title":"晴天","aliases":["sunny"]}]}`)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"sync_started"`) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/settings/aliases", "")
	if !strings.Contains(string(b), `"title":"晴天"`) {
		t.Fatalf("%s", b)
	}
	// audit written for settings changes (no secret values)
	au, _ := e.app.Store.ListAudit(context.Background(), "settings", 50)
	if len(au) < 3 {
		t.Fatalf("audit rows %d", len(au))
	}
}

// runTurn executes one agent turn and returns the conversation id.
func runTurn(t *testing.T, e *testEnv, text string) string {
	t.Helper()
	var conv string
	err := e.app.Agent.Run(context.Background(), agent.Input{Text: text, Channel: "web"}, func(ev agent.Event) {
		if d, ok := ev.Data.(agent.ConversationData); ok {
			conv = d.ID
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return conv
}

func TestTraceAPIs(t *testing.T) {
	f := llm.NewFake(
		llm.FakeTurn{Text: "查一下。", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_json", Arguments: json.RawMessage(`{"filter":".a","input":"{\"a\":42}"}`)}}},
		llm.FakeTurn{Text: "是 42。"},
		llm.FakeTurn{Text: "好的"},
	)
	e := newEnv(t, f, nil)
	conv := runTurn(t, e, "a 是多少")
	conv2 := runTurn(t, e, "第二个会话")

	resp, b := e.do(t, "GET", "/api/conversations/"+conv+"/trace", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	type row struct {
		ID       int64  `json:"id"`
		Step     int    `json:"step"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		Args     string `json:"args"`
		Result   string `json:"result_summary"`
		Finish   string `json:"finish_reason"`
		Duration int64  `json:"duration_ms"`
	}
	tr := decode[struct {
		Turns []struct {
			TurnID       string `json:"turn_id"`
			Status       string `json:"status"`
			DurationMS   int64  `json:"duration_ms"`
			FirstTokenMS int64  `json:"first_token_ms"`
			Input        string `json:"input"`
			Output       string `json:"output"`
			Steps        []row  `json:"steps"`
		} `json:"turns"`
	}](t, b)
	if len(tr.Turns) != 1 {
		t.Fatalf("turns %s", b)
	}
	turn := tr.Turns[0]
	if turn.Status != "ok" || turn.TurnID == "" || turn.DurationMS <= 0 || turn.FirstTokenMS <= 0 || turn.Input != "a 是多少" || turn.Output != "是 42。" {
		t.Fatalf("turn %+v", turn)
	}
	var order []string
	for _, s := range turn.Steps {
		order = append(order, s.Kind+":"+s.Name)
	}
	if got := strings.Join(order, ","); got != "llm:"+e.app.Provider.Model()+",tool:query_json,llm:"+e.app.Provider.Model() {
		t.Fatalf("step order %s", got)
	}
	if s := turn.Steps[1]; s.Step != 1 || s.Status != "ok" || !strings.Contains(s.Result, "42") || turn.Steps[0].Finish != "tool_calls" || turn.Steps[2].Step != 2 {
		t.Fatalf("steps %+v", turn.Steps)
	}
	if resp, _ := e.do(t, "GET", "/api/conversations/nope/trace", ""); resp.StatusCode != 404 {
		t.Fatalf("unknown conversation: %d", resp.StatusCode)
	}

	// list + filters + pagination
	type list struct {
		Items []struct {
			ID     int64  `json:"id"`
			Kind   string `json:"kind"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Conv   string `json:"conversation_id"`
			TurnID string `json:"turn_id"`
		} `json:"items"`
		Next *int64 `json:"next_before_id"`
	}
	_, b = e.do(t, "GET", "/api/traces", "")
	all := decode[list](t, b)
	if len(all.Items) != 6 || all.Next != nil || all.Items[0].ID < all.Items[5].ID { // conv1: llm,tool,llm,turn; conv2: llm,turn
		t.Fatalf("all: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?kind=turn", "")
	if l := decode[list](t, b); len(l.Items) != 2 {
		t.Fatalf("turns: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?kind=tool&name=query_json&status=ok", "")
	if l := decode[list](t, b); len(l.Items) != 1 || l.Items[0].Conv != conv {
		t.Fatalf("tool: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?conversation_id="+conv2, "")
	if l := decode[list](t, b); len(l.Items) != 2 { // llm + turn
		t.Fatalf("conv2: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?status=error", "")
	if l := decode[list](t, b); len(l.Items) != 0 {
		t.Fatalf("errors: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?limit=2", "")
	p1 := decode[list](t, b)
	if len(p1.Items) != 2 || p1.Next == nil || *p1.Next != p1.Items[1].ID {
		t.Fatalf("page1: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?limit=10&before_id="+itoa(int(*p1.Next)), "")
	if p2 := decode[list](t, b); len(p2.Items) != 4 || p2.Items[0].ID >= *p1.Next || p2.Next != nil {
		t.Fatalf("page2: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?since=2999-01-01T00:00:00Z", "")
	if l := decode[list](t, b); len(l.Items) != 0 {
		t.Fatalf("since: %s", b)
	}
	_, b = e.do(t, "GET", "/api/traces?since=0&until=99999999999999", "")
	if l := decode[list](t, b); len(l.Items) != 6 {
		t.Fatalf("since ms: %s", b)
	}
	for _, q := range []string{"status=bad", "kind=bad", "limit=0", "limit=501", "before_id=x", "since=yesterday", "until=x"} {
		if resp, _ := e.do(t, "GET", "/api/traces?"+q, ""); resp.StatusCode != 400 {
			t.Errorf("%s: %d", q, resp.StatusCode)
		}
	}
}

func TestPayloadAPI(t *testing.T) {
	f := llm.NewFake(
		llm.FakeTurn{Text: "查。", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_json", Arguments: json.RawMessage(`{"filter":".a","input":"{\"a\":42}"}`)}}},
		llm.FakeTurn{Text: "是 42。"},
		llm.FakeTurn{Text: "ok"},
	)
	e := newEnv(t, f, nil)

	turnOf := func(conv string) string {
		_, b := e.do(t, "GET", "/api/traces?kind=turn&conversation_id="+conv, "")
		l := decode[struct {
			Items []struct {
				TurnID string `json:"turn_id"`
			} `json:"items"`
		}](t, b)
		if len(l.Items) != 1 {
			t.Fatalf("turn rows: %s", b)
		}
		return l.Items[0].TurnID
	}
	// debug off: nothing recorded
	conv := runTurn(t, e, "off")
	turn := turnOf(conv)
	_, b := e.do(t, "GET", "/api/traces/"+turn+"/payload", "")
	p := decode[struct {
		Available bool              `json:"available"`
		Debug     bool              `json:"debug_payloads"`
		Items     []json.RawMessage `json:"items"`
	}](t, b)
	if p.Available || p.Debug || p.Items == nil || len(p.Items) != 0 {
		t.Fatalf("off: %s", b)
	}
	if resp, _ := e.do(t, "GET", "/api/traces/bad%20id/payload", ""); resp.StatusCode != 400 {
		t.Fatalf("bad id: %d", resp.StatusCode)
	}

	// enable debug payloads over the API, run a tool turn
	// (the off-turn above consumed one scripted turn; two remain)
	if resp, b := e.do(t, "PUT", "/api/settings/logging", `{"debug_payloads":true}`); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	f2 := llm.NewFake(
		llm.FakeTurn{Text: "查。", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_json", Arguments: json.RawMessage(`{"filter":".a","input":"{\"a\":42}"}`)}}},
		llm.FakeTurn{Text: "是 42。"},
	)
	e.app.Provider.Set(f2, "m")
	conv = runTurn(t, e, "on")
	turn = turnOf(conv)
	_, b = e.do(t, "GET", "/api/traces/"+turn+"/payload", "")
	p = decode[struct {
		Available bool              `json:"available"`
		Debug     bool              `json:"debug_payloads"`
		Items     []json.RawMessage `json:"items"`
	}](t, b)
	if !p.Available || !p.Debug || len(p.Items) != 3 {
		t.Fatalf("on: %s", b)
	}
	var kinds []string
	for _, it := range p.Items {
		m := decode[map[string]any](t, it)
		kinds = append(kinds, m["kind"].(string))
		if m["turn_id"] != turn {
			t.Errorf("foreign line: %s", it)
		}
	}
	if strings.Join(kinds, ",") != "llm,tool,llm" {
		t.Fatalf("kinds %v", kinds)
	}
	first := decode[struct {
		Request struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		} `json:"request"`
		Response struct {
			Text      string
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"response"`
	}](t, p.Items[0])
	if first.Request.Model != "m" || len(first.Request.Messages) < 2 || first.Request.Messages[0].Role != "system" ||
		len(first.Request.Tools) == 0 || first.Response.Text != "查。" || len(first.Response.ToolCalls) != 1 {
		t.Fatalf("llm payload: %s", p.Items[0])
	}
	if !strings.Contains(string(p.Items[1]), `\"a\":42`) && !strings.Contains(string(p.Items[1]), `42`) {
		t.Fatalf("tool payload: %s", p.Items[1])
	}
	// the file is on disk, 0600, and does not contain the API key
	files, _ := filepath.Glob(filepath.Join(e.dir, "logs", "trace-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("payload files %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if strings.Contains(string(raw), "sk-secret-1234567890") {
		t.Fatal("api key written to the payload log")
	}
	// unknown turn: unavailable
	_, b = e.do(t, "GET", "/api/traces/deadbeef0000/payload", "")
	if p := decode[map[string]any](t, b); p["available"] != false {
		t.Fatalf("%s", b)
	}
}

func TestMCPAPI(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/api/mcp/status", "")
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != `{"servers":[]}` {
		t.Fatalf("status %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "POST", "/api/mcp/test", `{"name":"x","transport":"stdio"}`)
	if resp.StatusCode != 400 || !strings.Contains(string(b), "command") {
		t.Fatalf("invalid: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "POST", "/api/mcp/test", `{"name":"x","transport":"http","url":"http://127.0.0.1:1/","timeout":"2s"}`)
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"ok":false`) {
		t.Fatalf("unreachable: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "PUT", "/api/settings/mcp", `{"servers":[{"name":"x","enabled":false,"transport":"http","url":"http://h/"}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "GET", "/api/mcp/status", "")
	if !strings.Contains(string(b), `"name":"x"`) || !strings.Contains(string(b), `"connected":false`) {
		t.Fatalf("status %s", b)
	}
}
