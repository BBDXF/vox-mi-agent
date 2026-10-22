package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
	"github.com/BBDXF/vox-mi-agent/internal/mcp"
)

func newFakeApp(t *testing.T, p llm.Provider) (*App, string) {
	t.Helper()
	t.Setenv("VOX_LLM_API_KEY", "")
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools.Dir = dir
	cfg.Tools.FzqPath, cfg.Tools.JqPath, cfg.Tools.YqPath = dir+"/fzq", dir+"/jq", dir+"/yq"
	cfg.Media.Folders = nil
	cfg.Media.SyncOnStart = false
	a, err := Build(cfg, WithProvider(p), WithLogger(logx.New("error", io.Discard)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, dir
}

func runTurn(t *testing.T, a *App, text string) {
	t.Helper()
	if err := a.Agent.Run(context.Background(), agent.Input{Text: text, Channel: "cli"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSectionRoundTripAndValidation(t *testing.T) {
	a, dir := newFakeApp(t, llm.NewFake())
	got, err := a.GetSection(context.Background(), "agent")
	if err != nil {
		t.Fatal(err)
	}
	if got["default_system_prompt"] != agent.DefaultSystemPrompt || got["system_prompt"] != "" ||
		got["max_steps"] != 6 || got["temperature"] != nil || got["history_messages"] != 40 || got["idle_reset"] != "30m" || got["context_tokens"] != 16000 {
		t.Fatalf("defaults: %+v", got)
	}
	tl := got["available_tools"].([]map[string]string)
	names := map[string]bool{}
	for _, x := range tl {
		names[x["name"]] = true
		if x["description"] == "" {
			t.Errorf("tool %s without description", x["name"])
		}
	}
	if !names["search_audio"] || !names["play_audio"] {
		t.Fatalf("available_tools %v", names)
	}

	res := put(t, a, "agent", `{"system_prompt":"Be terse.","max_steps":3,"temperature":0.7,"turn_timeout":"45s","history_messages":10,"tools_disabled":["set_volume","play_audio","play_audio"]}`)
	if res.RestartRequired {
		t.Error("agent must hot-apply")
	}
	c := reload(t, dir).Agent
	if c.SystemPrompt != "Be terse." || c.MaxSteps != 3 || c.Temperature == nil || *c.Temperature != 0.7 ||
		c.TurnTimeout != 45*time.Second || c.HistoryMessages != 10 || len(c.ToolsDisabled) > 2 {
		t.Fatalf("reloaded %+v", c)
	}
	put(t, a, "agent", `{"temperature":null}`)
	if reload(t, dir).Agent.Temperature != nil {
		t.Error("temperature null not persisted")
	}

	for js, key := range map[string]string{
		`{"max_steps":0}`:                 "max_steps",
		`{"temperature":3}`:               "temperature",
		`{"tools_disabled":["nope"]}`:     "tools_disabled",
		`{"history_messages":-1}`:         "history_messages",
		`{"turn_timeout":"5h"}`:           "turn_timeout",
		`{"idle_reset":"25h"}`:            "idle_reset",
		`{"context_tokens":100}`:          "context_tokens",
		`{"tools_disabled":"play_audio"}`: "tools_disabled",
	} {
		if fe := putFields(t, a, "agent", js); fe[key] == "" {
			t.Errorf("%s: want error on %s, got %v", js, key, fe)
		}
	}
}

func TestAgentPromptOverrideAndToolsDisabled(t *testing.T) {
	fake := llm.NewFake(llm.FakeTurn{Text: "one"}, llm.FakeTurn{Text: "two"}, llm.FakeTurn{Text: "three"})
	a, _ := newFakeApp(t, fake)

	runTurn(t, a, "hi")
	r := fake.Requests()[0]
	if r.Messages[0].Content != agent.DefaultSystemPrompt {
		t.Error("default prompt expected")
	}
	has := func(r llm.Request, name string) bool {
		for _, d := range r.Tools {
			if d.Name == name {
				return true
			}
		}
		return false
	}
	if !has(r, "play_audio") || !has(r, "search_audio") {
		t.Fatal("tools expected")
	}

	put(t, a, "agent", `{"system_prompt":"CUSTOM PROMPT","tools_disabled":["play_audio"],"temperature":0.9}`)
	runTurn(t, a, "hi again")
	r = fake.Requests()[1]
	if r.Messages[0].Role != llm.RoleSystem || r.Messages[0].Content != "CUSTOM PROMPT" {
		t.Errorf("system prompt = %q", r.Messages[0].Content)
	}
	if has(r, "play_audio") || !has(r, "search_audio") {
		t.Error("play_audio must be hidden, search_audio kept")
	}
	if r.Temperature == nil || *r.Temperature != 0.9 {
		t.Errorf("temperature %v", r.Temperature)
	}

	put(t, a, "agent", `{"system_prompt":"","tools_disabled":[]}`)
	runTurn(t, a, "third")
	r = fake.Requests()[2]
	if r.Messages[0].Content != agent.DefaultSystemPrompt || !has(r, "play_audio") {
		t.Error("reset to default expected")
	}
}

func TestMCPSectionRoundTripAndValidation(t *testing.T) {
	a, dir := newFakeApp(t, llm.NewFake())
	put(t, a, "mcp", `{"servers":[
	 {"name":"web","enabled":false,"transport":"http","url":"http://127.0.0.1:1/mcp","headers":{"Authorization":"Bearer abcdef123456"},"timeout":"5s","tools_allow":["x"]},
	 {"name":"local","enabled":false,"transport":"stdio","command":"echo","args":["a"],"env":{"K":"supersecret"}}]}`)
	c := reload(t, dir).MCP.Servers
	if len(c) != 2 || c[0].Headers["Authorization"] != "Bearer abcdef123456" || c[0].Timeout != 5*time.Second ||
		c[1].Command != "echo" || c[1].Env["K"] != "supersecret" || c[0].ToolsAllow[0] != "x" {
		t.Fatalf("round trip: %+v", c)
	}
	got, _ := a.GetSection(context.Background(), "mcp")
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "abcdef123456") || strings.Contains(string(b), "supersecret") ||
		!strings.Contains(string(b), "****3456") {
		t.Fatalf("secrets not masked: %s", b)
	}
	// sending the masked value back keeps the stored secret
	var items struct {
		Servers []json.RawMessage `json:"servers"`
	}
	json.Unmarshal(b, &items)
	put(t, a, "mcp", `{"servers":`+string(mustJSON(t, got["servers"]))+`}`)
	if reload(t, dir).MCP.Servers[0].Headers["Authorization"] != "Bearer abcdef123456" {
		t.Error("masked header overwrote the secret")
	}

	cases := map[string]string{
		`{"servers":[{"name":"a","transport":"ftp","url":"http://x"}]}`:                                               "servers[0].transport",
		`{"servers":[{"name":"a","transport":"http"}]}`:                                                               "servers[0].url",
		`{"servers":[{"name":"a","transport":"sse","url":"ws://x"}]}`:                                                 "servers[0].url",
		`{"servers":[{"name":"a","transport":"stdio"}]}`:                                                              "servers[0].command",
		`{"servers":[{"name":"a b","transport":"stdio","command":"x"}]}`:                                              "servers[0].name",
		`{"servers":[{"name":"a","transport":"stdio","command":"x"},{"name":"a","transport":"stdio","command":"y"}]}`: "servers[1].name",
		`{"servers":[{"name":"a","transport":"stdio","command":"x","timeout":"1h"}]}`:                                 "servers[0].timeout",
	}
	for js, key := range cases {
		if fe := putFields(t, a, "mcp", js); fe[key] == "" {
			t.Errorf("%s: want %s, got %v", js, key, fe)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type echoIn struct {
	Text string `json:"text"`
}

func startMCPServer(t *testing.T, auth *string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: "fake", Version: "1"}, nil)
	sdk.AddTool(srv, &sdk.Tool{Name: "echo", Description: "echo text back"},
		func(ctx context.Context, req *sdk.CallToolRequest, in echoIn) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo:" + in.Text}}}, nil, nil
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "secret.tool", Description: "hidden by allow list"},
		func(ctx context.Context, req *sdk.CallToolRequest, in echoIn) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "x"}}}, nil, nil
		})
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*auth = r.Header.Get("Authorization")
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestMCPToolsRegisteredAndCalled(t *testing.T) {
	var mu sync.Mutex
	var auth string
	ts := startMCPServer(t, &auth, &mu)

	fake := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mcp__my-srv__echo", Arguments: json.RawMessage(`{"text":"hello"}`)}}},
		llm.FakeTurn{Text: "done"},
	)
	a, _ := newFakeApp(t, fake)

	put(t, a, "mcp", `{"servers":[{"name":"my-srv","enabled":true,"transport":"http","url":"`+ts.URL+`","headers":{"Authorization":"Bearer tok"},"tools_allow":["echo"]}]}`)
	st := a.MCP.Status()
	if len(st) != 1 || !st[0].Connected || st[0].Error != "" || len(st[0].Tools) != 1 || st[0].Tools[0].Name != "mcp__my-srv__echo" {
		t.Fatalf("status %+v", st)
	}
	mu.Lock()
	if auth != "Bearer tok" {
		t.Errorf("auth header %q", auth)
	}
	mu.Unlock()
	if _, ok := a.Tools.Get("mcp__my-srv__secret_tool"); ok {
		t.Error("tools_allow ignored")
	}
	// built-in listing must not include MCP tools
	for _, x := range a.builtinTools() {
		if mcp.IsMCPTool(x["name"]) {
			t.Error("mcp tool in built-in list")
		}
	}

	runTurn(t, a, "call it")
	req := fake.Requests()[0]
	found := false
	for _, d := range req.Tools {
		if d.Name == "mcp__my-srv__echo" && strings.Contains(string(d.Parameters), "text") {
			found = true
		}
	}
	if !found {
		t.Fatal("mcp tool with schema not offered to the LLM")
	}
	last := fake.Requests()[1].Messages
	if tm := last[len(last)-1]; tm.Role != llm.RoleTool || !strings.Contains(tm.Content, "echo:hello") {
		t.Fatalf("tool result: %+v", tm)
	}

	// /api/mcp/test-style probe of an unsaved server; masked header resolved
	res, err := a.TestMCP(context.Background(), MCPServerDTO{Name: "my-srv", Transport: "http", URL: ts.URL,
		Headers: map[string]string{"Authorization": config.MaskSecret("Bearer tok")}})
	if err != nil || !res.OK || len(res.Tools) != 2 {
		t.Fatalf("test: %+v %v", res, err)
	}
	mu.Lock()
	if auth != "Bearer tok" {
		t.Errorf("masked header not resolved: %q", auth)
	}
	mu.Unlock()
	if _, err := a.TestMCP(context.Background(), MCPServerDTO{Name: "x", Transport: "http"}); err == nil {
		t.Error("invalid test body accepted")
	}
	if res, _ := a.TestMCP(context.Background(), MCPServerDTO{Name: "x", Transport: "http", URL: "http://127.0.0.1:1/", Timeout: "2s"}); res.OK || res.Error == "" {
		t.Errorf("unreachable server: %+v", res)
	}

	// disabling removes the tools and never fails the PUT for a dead server
	put(t, a, "mcp", `{"servers":[{"name":"my-srv","enabled":false,"transport":"http","url":"`+ts.URL+`"},{"name":"dead","enabled":true,"transport":"http","url":"http://127.0.0.1:1/","timeout":"2s"}]}`)
	if _, ok := a.Tools.Get("mcp__my-srv__echo"); ok {
		t.Error("tool still registered")
	}
	st = a.MCP.Status()
	if len(st) != 2 || st[0].Connected || st[1].Connected || st[1].Error == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestMCPToolName(t *testing.T) {
	if n := mcp.ToolName("a b", "c.d"); n != "mcp__a_b__c_d" {
		t.Error(n)
	}
	if n := mcp.ToolName("srv", strings.Repeat("x", 100)); len(n) != 64 {
		t.Error(len(n))
	}
}
