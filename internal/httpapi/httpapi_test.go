package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/app"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
	"github.com/BBDXF/vox-mi-agent/internal/store"
)

type testEnv struct {
	app *app.App
	dir string
	srv *httptest.Server
}

func toolsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VOX_TOOLS_DIR")
	if dir == "" {
		dir, _ = filepath.Abs("../../.tools")
	}
	return dir
}

func newApp(t *testing.T, p llm.Provider, mod func(*config.Config)) (*app.App, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VOX_LLM_API_KEY", "")
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools.Dir = toolsDir(t)
	cfg.Tools.FzqPath, cfg.Tools.JqPath, cfg.Tools.YqPath = "", "", ""
	cfg.Tools.FzqPath = filepath.Join(cfg.Tools.Dir, "fzq")
	cfg.Tools.JqPath = filepath.Join(cfg.Tools.Dir, "jq")
	cfg.Tools.YqPath = filepath.Join(cfg.Tools.Dir, "yq")
	cfg.Media.Folders = []string{filepath.Join(dir, "music")}
	cfg.Media.SyncOnStart = false
	cfg.LLM.APIKey = "sk-secret-1234567890"
	if mod != nil {
		mod(cfg)
	}
	a, err := app.Build(cfg, app.WithProvider(p), app.WithLogger(logx.New("error", io.Discard)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a, dir
}

func newEnv(t *testing.T, p llm.Provider, mod func(*config.Config), opts ...Option) *testEnv {
	t.Helper()
	a, dir := newApp(t, p, mod)
	static := fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	srv := httptest.NewServer(New(a, append([]Option{WithStatic(static)}, opts...)...).Handler())
	t.Cleanup(srv.Close)
	return &testEnv{app: a, dir: dir, srv: srv}
}

func (e *testEnv) do(t *testing.T, method, path, body string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

type sseEvent struct {
	Name string
	Data json.RawMessage
}

func parseSSE(t *testing.T, r io.Reader) []sseEvent {
	t.Helper()
	var evs []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.Name != "" {
				evs = append(evs, cur)
			}
			cur = sseEvent{}
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = json.RawMessage(strings.TrimPrefix(line, "data: "))
		}
	}
	return evs
}

func TestChatSSEWithToolCall(t *testing.T) {
	if _, err := os.Stat(filepath.Join(toolsDir(t), "jq")); err != nil {
		t.Skip("jq missing")
	}
	fake := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_json",
			Arguments: json.RawMessage(`{"filter":".a","input":"{\"a\":42}"}`)}}},
		llm.FakeTurn{Text: "The answer is 42."},
	)
	e := newEnv(t, fake, nil)

	req, _ := http.NewRequest("POST", e.srv.URL+"/sse/chat", strings.NewReader(`{"message":"what is a?"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type %q", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("bad streaming headers: %v", resp.Header)
	}
	evs := parseSSE(t, resp.Body)

	var names []string
	var convID, text string
	var toolRes struct {
		Name   string          `json:"name"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	for _, ev := range evs {
		names = append(names, ev.Name)
		switch ev.Name {
		case "conversation":
			var d struct{ ID string }
			json.Unmarshal(ev.Data, &d)
			convID = d.ID
		case "token":
			var d struct{ Text string }
			json.Unmarshal(ev.Data, &d)
			text += d.Text
		case "tool_call":
			var d struct {
				ID, Name  string
				Arguments map[string]any
			}
			if err := json.Unmarshal(ev.Data, &d); err != nil || d.Name != "query_json" || d.ID != "c1" {
				t.Errorf("tool_call %s (%v)", ev.Data, err)
			}
		case "tool_result":
			json.Unmarshal(ev.Data, &toolRes)
		}
	}
	if names[0] != "conversation" || names[len(names)-1] != "done" {
		t.Fatalf("event order: %v", names)
	}
	for _, want := range []string{"token", "sentence", "tool_call", "tool_result", "done"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("missing %s event in %v", want, names)
		}
	}
	if text != "The answer is 42." {
		t.Errorf("text = %q", text)
	}
	if toolRes.Name != "query_json" || toolRes.Error != "" || !strings.Contains(string(toolRes.Result), "42") {
		t.Errorf("tool_result = %+v (%s)", toolRes, toolRes.Result)
	}
	if convID == "" {
		t.Fatal("no conversation id")
	}

	// persisted + listed
	_, b := e.do(t, "GET", "/api/conversations/"+convID+"/messages", "")
	var msgs struct {
		Items []struct {
			ID        int64  `json:"id"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID, Name  string
				Arguments map[string]any
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
			Name       string `json:"name"`
			CreatedAt  string `json:"created_at"`
		}
	}
	if err := json.Unmarshal(b, &msgs); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	roles := []string{}
	for _, m := range msgs.Items {
		roles = append(roles, m.Role)
		if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
			t.Errorf("created_at %q: %v", m.CreatedAt, err)
		}
	}
	if strings.Join(roles, ",") != "user,assistant,tool,assistant" {
		t.Fatalf("roles = %v", roles)
	}
	if tc := msgs.Items[1].ToolCalls; len(tc) != 1 || tc[0].Name != "query_json" || tc[0].Arguments["filter"] != ".a" {
		t.Errorf("tool_calls = %+v", tc)
	}
	if msgs.Items[2].ToolCallID != "c1" || msgs.Items[2].Name != "query_json" {
		t.Errorf("tool msg = %+v", msgs.Items[2])
	}

	_, b = e.do(t, "GET", "/api/conversations", "")
	var list struct {
		Items []struct{ ID, Title, Channel, CreatedAt, UpdatedAt string } `json:"items"`
	}
	json.Unmarshal(b, &list)
	if len(list.Items) != 1 || list.Items[0].ID != convID || list.Items[0].Channel != "web" {
		t.Errorf("list = %s", b)
	}
}

func TestChatErrors(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, _ := e.do(t, "POST", "/sse/chat", `{"message":"  "}`)
	if resp.StatusCode != 400 {
		t.Errorf("empty message: %d", resp.StatusCode)
	}
	resp, _ = e.do(t, "POST", "/sse/chat", `{"conversation_id":"nope","message":"hi"}`)
	if resp.StatusCode != 404 {
		t.Errorf("unknown conv: %d", resp.StatusCode)
	}
	// LLM failure surfaces as an SSE error event
	resp, b := e.do(t, "POST", "/sse/chat", `{"message":"hi"}`)
	evs := parseSSE(t, bytes.NewReader(b))
	if resp.StatusCode != 200 || len(evs) == 0 || evs[len(evs)-1].Name != "error" {
		t.Errorf("want trailing error event, got %d %s", resp.StatusCode, b)
	}
}

func TestChatClientDisconnectCancels(t *testing.T) {
	// A provider that blocks until ctx is cancelled.
	blocked := make(chan struct{})
	p := blockingProvider{started: blocked}
	e := newEnv(t, p, nil)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", e.srv.URL+"/sse/chat", strings.NewReader(`{"message":"hi"}`))
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("provider never called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not return")
	}
	select {
	case <-p.ctxDone(t):
	case <-time.After(5 * time.Second):
		t.Fatal("server-side context not cancelled")
	}
}

type blockingProvider struct{ started chan struct{} }

var cancelledCh = make(chan struct{}, 1)

func (blockingProvider) Name() string { return "block" }
func (b blockingProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	close(b.started)
	out := make(chan llm.Event)
	go func() {
		<-ctx.Done()
		cancelledCh <- struct{}{}
		close(out)
	}()
	return out, nil
}
func (blockingProvider) ctxDone(*testing.T) <-chan struct{} { return cancelledCh }

func TestBasePathStripping(t *testing.T) {
	e := newEnv(t, llm.NewFake(), func(c *config.Config) { c.Server.BasePath = "/app/vox" })
	for _, p := range []string{"/healthz", "/app/vox/healthz"} {
		resp, b := e.do(t, "GET", p, "")
		if resp.StatusCode != 200 || !strings.Contains(string(b), `"status":"ok"`) {
			t.Errorf("%s: %d %s", p, resp.StatusCode, b)
		}
	}
	resp, b := e.do(t, "GET", "/app/vox/api/media/status", "")
	if resp.StatusCode != 200 {
		t.Errorf("api under base: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "GET", "/app/vox/api/nope", "")
	if resp.StatusCode != 404 || !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Errorf("api 404 under base: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "GET", "/app/vox", "")
	if resp.StatusCode != 200 || !strings.Contains(string(b), "spa") {
		t.Errorf("base root: %d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "GET", "/app/voxel/api/media/status", "") // not a prefix match
	if resp.StatusCode != 200 && resp.StatusCode != 404 {
		t.Errorf("unexpected %d", resp.StatusCode)
	}
	if resp.StatusCode == 200 && strings.Contains(string(b), "running") {
		t.Errorf("/app/voxel must not be stripped as /app/vox")
	}
}

func TestAdminAuth(t *testing.T) {
	e := newEnv(t, llm.NewFake(), func(c *config.Config) { c.Server.Auth.RequireAdmin = true })
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/media/status", ""},
		{"GET", "/api/nope", ""},
		{"POST", "/sse/chat", `{"message":"x"}`},
		{"PUT", "/api/settings/llm", `{}`},
	} {
		resp, b := e.do(t, tc.method, tc.path, tc.body)
		if resp.StatusCode != 403 {
			t.Errorf("%s %s without admin: %d %s", tc.method, tc.path, resp.StatusCode, b)
		}
		resp, _ = e.do(t, tc.method, tc.path, tc.body, "X-Trim-Isadmin", "false")
		if resp.StatusCode != 403 {
			t.Errorf("%s %s admin=false: %d", tc.method, tc.path, resp.StatusCode)
		}
	}
	resp, _ := e.do(t, "GET", "/api/media/status", "", "X-Trim-Isadmin", "true")
	if resp.StatusCode != 200 {
		t.Errorf("admin: %d", resp.StatusCode)
	}
	// healthz and static are exempt (container healthcheck has no headers)
	if resp, _ := e.do(t, "GET", "/healthz", ""); resp.StatusCode != 200 {
		t.Errorf("healthz: %d", resp.StatusCode)
	}
}

func TestAPI404JSONVsSPAFallback(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	for _, p := range []string{"/api/nope", "/api/conversations/x/y/z", "/sse/nope", "/api"} {
		resp, b := e.do(t, "GET", p, "")
		var m map[string]string
		if resp.StatusCode != 404 || !strings.Contains(resp.Header.Get("Content-Type"), "application/json") ||
			json.Unmarshal(b, &m) != nil || m["error"] == "" {
			t.Errorf("%s: %d %s %s", p, resp.StatusCode, resp.Header.Get("Content-Type"), b)
		}
	}
	// 405 on known API path is JSON too
	if resp, b := e.do(t, "DELETE", "/api/settings", ""); resp.StatusCode != 405 || !json.Valid(b) {
		t.Errorf("405: %d %s", resp.StatusCode, b)
	}
	for _, p := range []string{"/", "/chat/123", "/settings"} {
		resp, b := e.do(t, "GET", p, "")
		if resp.StatusCode != 200 || !strings.Contains(string(b), "spa") {
			t.Errorf("SPA %s: %d %s", p, resp.StatusCode, b)
		}
	}
	if resp, b := e.do(t, "GET", "/assets/app.js", ""); resp.StatusCode != 200 || !strings.Contains(string(b), "console.log") {
		t.Errorf("asset: %d %s", resp.StatusCode, b)
	}
	if resp, _ := e.do(t, "GET", "/assets/missing.js", ""); resp.StatusCode != 404 {
		t.Errorf("missing asset: %d", resp.StatusCode)
	}
}

func TestHealthzAndMedia(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/healthz", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var h struct {
		Status string
		Tools  map[string]struct {
			Path    string
			OK      bool
			Version string
			Error   string
		}
		Media map[string]any
	}
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || len(h.Tools) != 3 || h.Media == nil {
		t.Fatalf("healthz = %s", b)
	}
	for name, ts := range h.Tools {
		if ts.Path == "" {
			t.Errorf("%s: empty path", name)
		}
		if _, err := os.Stat(ts.Path); err == nil && (!ts.OK || ts.Version == "") {
			t.Errorf("%s present but not ok: %+v", name, ts)
		}
	}

	resp, b = e.do(t, "GET", "/api/media/status", "")
	var st map[string]any
	json.Unmarshal(b, &st)
	if _, ok := st["running"]; resp.StatusCode != 200 || !ok {
		t.Errorf("media status: %s", b)
	}
	if _, err := os.Stat(filepath.Join(toolsDir(t), "fzq")); err != nil {
		return
	}
	resp, b = e.do(t, "POST", "/api/media/sync", "")
	if resp.StatusCode != 202 {
		t.Fatalf("sync: %d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, b = e.do(t, "GET", "/api/media/status", "")
		json.Unmarshal(b, &st)
		if st["running"] == false && st["result"] != "" && st["result"] != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("sync did not finish: %s", b)
}

func TestConversationsPagination(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	var ids []string
	for i := 0; i < 3; i++ {
		resp, b := e.do(t, "POST", "/api/conversations", `{"title":"t`+string(rune('0'+i))+`"}`)
		if resp.StatusCode != 201 {
			t.Fatalf("create: %d %s", resp.StatusCode, b)
		}
		var c struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Channel   string `json:"channel"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		}
		json.Unmarshal(b, &c)
		if c.Channel != "web" || c.Title == "" {
			t.Fatalf("conv %s", b)
		}
		if _, err := time.Parse(time.RFC3339, c.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		time.Sleep(3 * time.Millisecond) // distinct updated_at (ms resolution)
	}
	// empty body is allowed
	if resp, _ := e.do(t, "POST", "/api/conversations", ""); resp.StatusCode != 201 {
		t.Errorf("empty body: %d", resp.StatusCode)
	}
	time.Sleep(3 * time.Millisecond)

	type page struct {
		Items      []struct{ ID string } `json:"items"`
		NextBefore string                `json:"next_before"`
	}
	var p1, p2 page
	_, b := e.do(t, "GET", "/api/conversations?limit=2", "")
	json.Unmarshal(b, &p1)
	if len(p1.Items) != 2 || p1.NextBefore != p1.Items[1].ID {
		t.Fatalf("page1 = %s", b)
	}
	_, b = e.do(t, "GET", "/api/conversations?limit=2&before="+p1.NextBefore, "")
	json.Unmarshal(b, &p2)
	if len(p2.Items) != 2 || p2.NextBefore != "" {
		t.Fatalf("page2 = %s", b)
	}
	if resp, _ := e.do(t, "GET", "/api/conversations?before=bogus", ""); resp.StatusCode != 400 {
		t.Errorf("bogus before: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "GET", "/api/conversations?limit=0", ""); resp.StatusCode != 400 {
		t.Errorf("limit 0: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "GET", "/api/conversations/nope/messages", ""); resp.StatusCode != 404 {
		t.Errorf("messages of unknown conv: %d", resp.StatusCode)
	}
	_, b = e.do(t, "GET", "/api/conversations/"+ids[0]+"/messages", "")
	if strings.TrimSpace(string(b)) != `{"items":[]}` {
		t.Errorf("empty messages = %s", b)
	}
}

func TestSettingsRedactionAndPut(t *testing.T) {
	e := newEnv(t, llm.NewFake(), func(c *config.Config) {
		c.Sources = []config.Source{{Name: "people", File: "people.json", Format: "json"}}
	})
	_, b := e.do(t, "GET", "/api/settings", "")
	if strings.Contains(string(b), "sk-secret-1234567890") || strings.Contains(string(b), "secret") {
		t.Fatalf("plaintext key leaked: %s", b)
	}
	var s struct {
		LLM struct {
			BaseURL     string  `json:"base_url"`
			Model       string  `json:"model"`
			Masked      string  `json:"api_key_masked"`
			Temperature float64 `json:"temperature"`
		} `json:"llm"`
		Tools   map[string]struct{ Path string }      `json:"tools"`
		Sources []struct{ Name, Format, File string } `json:"sources"`
		Media   struct {
			Folders      []string `json:"folders"`
			SyncInterval string   `json:"sync_interval"`
		} `json:"media"`
		Speaker struct{ Driver string } `json:"speaker"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	if s.LLM.Masked != "****7890" || s.LLM.Model == "" || len(s.Tools) != 3 ||
		len(s.Sources) != 1 || s.Sources[0].Name != "people" || len(s.Media.Folders) != 1 ||
		s.Media.SyncInterval == "" || s.Speaker.Driver != "mock" {
		t.Fatalf("settings = %s", b)
	}

	// validation
	for _, body := range []string{
		`{"base_url":"ftp://x","model":"m","temperature":0.5}`,
		`{"base_url":"http://x","model":"","temperature":0.5}`,
		`{"base_url":"http://x","model":"m","temperature":5}`,
		`not json`,
	} {
		if resp, _ := e.do(t, "PUT", "/api/settings/llm", body); resp.StatusCode != 400 {
			t.Errorf("%s: %d", body, resp.StatusCode)
		}
	}

	// PUT without api_key keeps the key, persists to config.yaml
	resp, b := e.do(t, "PUT", "/api/settings/llm", `{"base_url":"http://127.0.0.1:9/v1","model":"qwen-x","api_key":"","temperature":0.7}`)
	if resp.StatusCode != 204 {
		t.Fatalf("put: %d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/settings", "")
	json.Unmarshal(b, &s)
	if s.LLM.BaseURL != "http://127.0.0.1:9/v1" || s.LLM.Model != "qwen-x" || s.LLM.Temperature != 0.7 || s.LLM.Masked != "****7890" {
		t.Fatalf("after put: %s", b)
	}

	// new key
	if resp, b = e.do(t, "PUT", "/api/settings/llm", `{"base_url":"http://127.0.0.1:9/v1","model":"qwen-x","api_key":"sk-new-abcdef","temperature":0.7}`); resp.StatusCode != 204 {
		t.Fatalf("put2: %d %s", resp.StatusCode, b)
	}
	_, b = e.do(t, "GET", "/api/settings", "")
	if strings.Contains(string(b), "sk-new-abcdef") || !strings.Contains(string(b), "****cdef") {
		t.Errorf("masking after key change: %s", b)
	}

	// file is authoritative: reload from disk
	t.Setenv("VOX_LLM_API_KEY", "")
	cfg, err := config.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.BaseURL != "http://127.0.0.1:9/v1" || cfg.LLM.Model != "qwen-x" || cfg.LLM.Temperature != 0.7 || cfg.LLM.APIKey != "sk-new-abcdef" {
		t.Errorf("reloaded llm = %+v", cfg.LLM)
	}
	if cfg.Server.Listen == "" || cfg.Search.MinScore != 65 || cfg.LLM.Timeout != 60*time.Second {
		t.Errorf("other sections damaged: %+v", cfg)
	}
	raw, _ := os.ReadFile(filepath.Join(e.dir, config.FileName))
	if !strings.Contains(string(raw), "# vox-mi-agent default configuration") {
		t.Errorf("comments lost:\n%s", raw)
	}
	if fi, _ := os.Stat(filepath.Join(e.dir, config.FileName)); fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", fi.Mode())
	}
}

func TestUnixSocketServeAndHealthcheck(t *testing.T) {
	a, _ := newApp(t, llm.NewFake(), nil)
	sock := filepath.Join(t.TempDir(), "app.sock")
	// stale non-socket file must be refused; stale socket must be replaced
	if err := os.WriteFile(sock, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen("unix://" + sock); err == nil {
		t.Fatal("expected error for non-socket file")
	}
	os.Remove(sock)
	stale, err := Listen("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	stale.(interface{ SetUnlinkOnClose(bool) }).SetUnlinkOnClose(false)
	stale.Close() // leaves a stale socket file behind
	if fi, err := os.Lstat(sock); err != nil || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("expected stale socket, %v", err)
	}

	ln, err := Listen("unix://" + sock)
	if err != nil {
		t.Fatalf("listen over stale socket: %v", err)
	}
	if fi, _ := os.Stat(sock); fi.Mode().Perm() != 0o666 {
		t.Errorf("socket mode %v", fi.Mode().Perm())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, New(a, WithStatic(fstest.MapFS{"index.html": {Data: []byte("x")}})).Handler(), a.Log)
	}()

	if err := Healthcheck(context.Background(), "unix://"+sock); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no graceful shutdown")
	}
	if err := Healthcheck(context.Background(), "unix://"+sock); err == nil {
		t.Error("healthcheck should fail after shutdown")
	}
}

func TestHealthcheckTCP(t *testing.T) {
	a, _ := newApp(t, llm.NewFake(), nil)
	ln, err := Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, New(a).Handler(), a.Log) }()
	if err := Healthcheck(context.Background(), "tcp://"+ln.Addr().String()); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
	cancel()
	<-done
}

// fakeOpenAI is a tiny OpenAI-compatible server recording the last auth header.
type fakeOpenAI struct {
	srv      *httptest.Server
	lastAuth string
	lastBody string
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		if f.lastAuth == "Bearer bad" {
			http.Error(w, "unauthorized", 401)
			return
		}
		io.WriteString(w, `{"data":[{"id":"zeta"},{"id":"alpha"}]}`)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		f.lastBody = string(b)
		if f.lastAuth == "Bearer bad" {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestLLMProvidersEndpoint(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/api/llm/providers", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var r struct {
		Items []struct {
			ID              string         `json:"id"`
			Name            string         `json:"name"`
			BaseURL         string         `json:"base_url"`
			DefaultModel    string         `json:"default_model"`
			KeyRequired     bool           `json:"key_required"`
			BaseURLEditable bool           `json:"base_url_editable"`
			DocsURL         string         `json:"docs_url"`
			ExtraBody       map[string]any `json:"extra_body"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 8 || r.Items[0].ID != "opencode-zen" || r.Items[0].DocsURL != "https://opencode.ai/docs/zen" ||
		r.Items[6].ID != "ollama" || r.Items[6].KeyRequired || !r.Items[6].BaseURLEditable {
		t.Errorf("providers = %s", b)
	}
	for _, it := range r.Items {
		if it.ID == "siliconflow" && (it.DefaultModel != "Qwen/Qwen3-8B" || it.ExtraBody["enable_thinking"] != false) {
			t.Errorf("siliconflow = %+v", it)
		}
	}
}

func TestSettingsProviderAndKeySemantics(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil) // provider custom, key sk-secret-1234567890
	type settings struct {
		LLM struct {
			Provider string `json:"provider"`
			BaseURL  string `json:"base_url"`
			HasKey   bool   `json:"has_api_key"`
			Masked   string `json:"api_key_masked"`
		} `json:"llm"`
	}
	get := func() settings {
		_, b := e.do(t, "GET", "/api/settings", "")
		if strings.Contains(string(b), "sk-secret-1234567890") {
			t.Fatalf("key leaked: %s", b)
		}
		var s settings
		json.Unmarshal(b, &s)
		return s
	}
	if s := get(); s.LLM.Provider != "custom" || !s.LLM.HasKey {
		t.Fatalf("initial: %+v", s)
	}
	// switching to a key-required preset without key -> 400, nothing changed
	resp, b := e.do(t, "PUT", "/api/settings/llm", `{"provider":"deepseek","model":"deepseek-chat","temperature":0.3}`)
	if resp.StatusCode != 400 || !strings.Contains(string(b), "API key") {
		t.Fatalf("expected 400 key required: %d %s", resp.StatusCode, b)
	}
	if s := get(); s.LLM.Provider != "custom" || !s.LLM.HasKey {
		t.Fatalf("state changed after failed put: %+v", s)
	}
	// unknown provider
	if resp, _ := e.do(t, "PUT", "/api/settings/llm", `{"provider":"nope","base_url":"http://x/v1","model":"m"}`); resp.StatusCode != 400 {
		t.Errorf("unknown provider: %d", resp.StatusCode)
	}
	// switch to ollama (key optional) without key -> old vendor's key cleared; base_url editable
	resp, b = e.do(t, "PUT", "/api/settings/llm", `{"provider":"ollama","base_url":"http://nas:11434/v1","model":"qwen2.5","temperature":0.3}`)
	if resp.StatusCode != 204 {
		t.Fatalf("ollama: %d %s", resp.StatusCode, b)
	}
	if s := get(); s.LLM.Provider != "ollama" || s.LLM.HasKey || s.LLM.Masked != "" || s.LLM.BaseURL != "http://nas:11434/v1" {
		t.Fatalf("after ollama: %+v", s)
	}
	// deepseek with a key: base_url forced to preset
	resp, b = e.do(t, "PUT", "/api/settings/llm", `{"provider":"deepseek","base_url":"http://evil/v1","model":"deepseek-chat","api_key":"sk-ds-abcd1234","temperature":0.3}`)
	if resp.StatusCode != 204 {
		t.Fatalf("deepseek: %d %s", resp.StatusCode, b)
	}
	if s := get(); s.LLM.Provider != "deepseek" || !s.LLM.HasKey || s.LLM.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("after deepseek: %+v", s)
	}
	// same provider, empty key keeps it
	if resp, _ = e.do(t, "PUT", "/api/settings/llm", `{"provider":"deepseek","model":"deepseek-reasoner","temperature":0.5}`); resp.StatusCode != 204 {
		t.Fatalf("keep: %d", resp.StatusCode)
	}
	if s := get(); !s.LLM.HasKey || s.LLM.Masked != "****1234" {
		t.Fatalf("key not kept: %+v", s)
	}
	// persisted & reloadable; file has provider, base_url is the preset's
	t.Setenv("VOX_LLM_API_KEY", "")
	cfg, err := config.Load(e.dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "deepseek" || cfg.LLM.APIKey != "sk-ds-abcd1234" || cfg.LLM.Model != "deepseek-reasoner" {
		t.Errorf("reloaded = %+v", cfg.LLM)
	}
	// switching away with an empty key clears the stored key on disk too
	if resp, _ = e.do(t, "PUT", "/api/settings/llm", `{"provider":"custom","base_url":"http://x/v1","model":"m","temperature":0.3}`); resp.StatusCode != 204 {
		t.Fatalf("custom: %d", resp.StatusCode)
	}
	cfg, _ = config.Load(e.dir)
	if cfg.LLM.Provider != "custom" || cfg.LLM.APIKey != "" {
		t.Errorf("reloaded after switch = %+v", cfg.LLM)
	}
}

func TestLLMModelsEndpoint(t *testing.T) {
	f := newFakeOpenAI(t)
	e := newEnv(t, llm.NewFake(), func(c *config.Config) { c.LLM.BaseURL = f.srv.URL + "/v1" })
	base := f.srv.URL + "/v1"

	// same provider + empty key -> stored key
	resp, b := e.do(t, "POST", "/api/llm/models", `{"provider":"custom","base_url":"`+base+`"}`)
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != `{"items":[{"id":"alpha"},{"id":"zeta"}]}` {
		t.Fatalf("models: %d %s", resp.StatusCode, b)
	}
	if f.lastAuth != "Bearer sk-secret-1234567890" {
		t.Errorf("auth = %q", f.lastAuth)
	}
	// different provider + empty key -> stored key NOT reused
	resp, _ = e.do(t, "POST", "/api/llm/models", `{"provider":"ollama","base_url":"`+base+`"}`)
	if resp.StatusCode != 200 || f.lastAuth != "" {
		t.Errorf("ollama: %d auth=%q", resp.StatusCode, f.lastAuth)
	}
	// explicit key wins
	e.do(t, "POST", "/api/llm/models", `{"provider":"custom","base_url":"`+base+`","api_key":"mine"}`)
	if f.lastAuth != "Bearer mine" {
		t.Errorf("auth = %q", f.lastAuth)
	}
	// upstream failure -> 502 {error}
	resp, b = e.do(t, "POST", "/api/llm/models", `{"provider":"custom","base_url":"`+base+`","api_key":"bad"}`)
	if resp.StatusCode != 502 || !strings.Contains(string(b), `"error"`) || !strings.Contains(string(b), "401") {
		t.Errorf("bad key: %d %s", resp.StatusCode, b)
	}
	// validation
	if resp, _ = e.do(t, "POST", "/api/llm/models", `{"provider":"nope"}`); resp.StatusCode != 400 {
		t.Errorf("unknown provider: %d", resp.StatusCode)
	}
	if resp, _ = e.do(t, "POST", "/api/llm/models", `{"provider":"custom","base_url":"ftp://x"}`); resp.StatusCode != 400 {
		t.Errorf("bad url: %d", resp.StatusCode)
	}
}

func TestLLMTestEndpoint(t *testing.T) {
	f := newFakeOpenAI(t)
	e := newEnv(t, llm.NewFake(), func(c *config.Config) { c.LLM.BaseURL = f.srv.URL + "/v1" })
	base := f.srv.URL + "/v1"

	resp, b := e.do(t, "POST", "/api/llm/test", `{"provider":"custom","base_url":"`+base+`","model":"m1"}`)
	var r struct {
		OK        bool   `json:"ok"`
		LatencyMS *int64 `json:"latency_ms"`
		Reply     string `json:"reply"`
		Error     string `json:"error"`
	}
	json.Unmarshal(b, &r)
	if resp.StatusCode != 200 || !r.OK || r.Reply != "ok" || r.LatencyMS == nil {
		t.Fatalf("test: %d %s", resp.StatusCode, b)
	}
	if f.lastAuth != "Bearer sk-secret-1234567890" {
		t.Errorf("auth = %q", f.lastAuth)
	}
	if !strings.Contains(f.lastBody, `"max_tokens":20`) || strings.Contains(f.lastBody, `"tools"`) {
		t.Errorf("body = %s", f.lastBody)
	}
	// upstream error
	_, b = e.do(t, "POST", "/api/llm/test", `{"provider":"custom","base_url":"`+base+`","model":"m1","api_key":"bad"}`)
	r.OK, r.Error = true, ""
	json.Unmarshal(b, &r)
	if r.OK || !strings.Contains(r.Error, "401") {
		t.Errorf("bad key: %s", b)
	}
	// missing model / missing required key (preset base_url is not contacted)
	_, b = e.do(t, "POST", "/api/llm/test", `{"provider":"custom","base_url":"`+base+`","model":""}`)
	if strings.Contains(string(b), `"ok":true`) || !strings.Contains(string(b), "model") {
		t.Errorf("no model: %s", b)
	}
	_, b = e.do(t, "POST", "/api/llm/test", `{"provider":"deepseek","model":"deepseek-chat"}`)
	if !strings.Contains(string(b), `"ok":false`) || !strings.Contains(string(b), "API key") {
		t.Errorf("key required: %s", b)
	}
	// test must not change live settings
	_, b = e.do(t, "GET", "/api/settings", "")
	if !strings.Contains(string(b), `"provider":"custom"`) {
		t.Errorf("settings changed: %s", b)
	}
}

type convBlockProvider struct{ started chan struct{} }

func (convBlockProvider) Name() string { return "convblock" }
func (b convBlockProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	close(b.started)
	out := make(chan llm.Event)
	go func() { <-ctx.Done(); close(out) }()
	return out, nil
}

func TestDeleteConversations(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	create := func() string {
		t.Helper()
		resp, b := e.do(t, "POST", "/api/conversations", `{"title":"x"}`)
		if resp.StatusCode != 201 {
			t.Fatalf("create: %d %s", resp.StatusCode, b)
		}
		var c struct {
			ID string `json:"id"`
		}
		json.Unmarshal(b, &c)
		return c.ID
	}
	ctx := context.Background()
	a, b, c := create(), create(), create()
	if _, err := e.app.Store.AppendMessage(ctx, a, store.Message{Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}

	resp, body := e.do(t, "DELETE", "/api/conversations/"+a, "")
	if resp.StatusCode != 204 || len(body) != 0 {
		t.Fatalf("delete: %d %q", resp.StatusCode, body)
	}
	for _, path := range []string{"/api/conversations/" + a, "/api/conversations/nope"} {
		if resp, _ := e.do(t, "DELETE", path, ""); resp.StatusCode != 404 {
			t.Fatalf("DELETE %s: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := e.do(t, "GET", "/api/conversations/"+a+"/messages", ""); resp.StatusCode != 404 {
		t.Fatalf("messages of deleted: %d", resp.StatusCode)
	}
	if _, err := e.app.Store.GetConversation(ctx, b); err != nil {
		t.Fatalf("b must survive: %v", err)
	}

	resp, body = e.do(t, "DELETE", "/api/conversations", "")
	var del struct {
		Deleted int `json:"deleted"`
	}
	json.Unmarshal(body, &del)
	if resp.StatusCode != 200 || del.Deleted != 2 {
		t.Fatalf("delete all: %d %s", resp.StatusCode, body)
	}
	if _, err := e.app.Store.GetConversation(ctx, c); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("c: %v", err)
	}
	_, body = e.do(t, "DELETE", "/api/conversations", "")
	json.Unmarshal(body, &del)
	if del.Deleted != 0 {
		t.Fatalf("empty: %s", body)
	}
}

func TestDeleteConversationCancelsRunningTurn(t *testing.T) {
	started := make(chan struct{})
	e := newEnv(t, convBlockProvider{started: started}, nil)
	_, b := e.do(t, "POST", "/api/conversations", `{"title":"x"}`)
	var c struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &c)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Post(e.srv.URL+"/sse/chat", "application/json",
			strings.NewReader(`{"conversation_id":"`+c.ID+`","message":"hi"}`))
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never started")
	}
	if resp, body := e.do(t, "DELETE", "/api/conversations/"+c.ID, ""); resp.StatusCode != 204 {
		t.Fatalf("delete: %d %s", resp.StatusCode, body)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("running turn was not cancelled")
	}
	if _, err := e.app.Store.GetConversation(context.Background(), c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("conversation still exists: %v", err)
	}
}

func TestVersionEndpoint(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/api/version", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "commit", "build_date", "go_version", "os", "arch", "started_at", "uptime_s", "tools", "data_dir"} {
		if _, ok := v[k]; !ok {
			t.Errorf("missing %q in %s", k, b)
		}
	}
}
