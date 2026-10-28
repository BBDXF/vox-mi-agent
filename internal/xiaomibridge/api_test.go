package xiaomibridge

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rig runs a real bridge (tailer, pusher, API) on a loopback port with fake
// device commands.
type rig struct {
	t      *testing.T
	base   string
	dir    string
	cfg    string
	logf   string
	cancel context.CancelFunc
	done   chan error
}

func fakeDevice(t *testing.T, dir string) *Device {
	t.Helper()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rec := `{ echo "$0"; for a in "$@"; do echo "$a"; done; echo --; } >> ` + filepath.Join(dir, "calls.log") + "\n"
	return &Device{
		Ubus:     write("ubus", rec+`echo '{"code": 0}'`),
		Mphelper: write("mphelper", rec+`[ "$1" = mute_stat ] && echo 1; exit 0`),
		// Like the real tts_play.sh: a player child does the playing.
		TTSPlay: write("tts_play.sh", `printf '%s\n' "$1" >> `+filepath.Join(dir, "tts.log")+"\n"+
			"sleep 30 &\necho $! >> "+filepath.Join(dir, "children")+"\nwait\n"),
		AivsInit: write("mico_aivs_lab", rec),
	}
}

func startRig(t *testing.T, dir string) *rig {
	t.Helper()
	r := &rig{t: t, dir: dir, cfg: filepath.Join(dir, "conf", "config.json"), logf: filepath.Join(dir, "instruction.log")}
	b, err := New(Options{ConfigPath: r.cfg, LogPath: r.logf, Poll: time.Millisecond, DefaultDevice: "lx06", Dev: fakeDevice(t, dir), Log: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r.base = "http://" + ln.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan error, 1)
	go func() { r.done <- b.Serve(ctx, ln) }()
	t.Cleanup(r.stop)
	time.Sleep(20 * time.Millisecond) // tail starts at the current end
	return r
}

func (r *rig) stop() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	r.cancel = nil
	if err := <-r.done; err != nil {
		r.t.Errorf("Serve: %v", err)
	}
}

// call sends a request; user "" means no credentials.
func (r *rig) call(method, path, user, pass, body string) (int, map[string]any) {
	r.t.Helper()
	req, _ := http.NewRequest(method, r.base+path, strings.NewReader(body))
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (r *rig) admin(method, path, body string) map[string]any {
	r.t.Helper()
	code, out := r.call(method, path, DefaultUsername, DefaultPassword, body)
	if code != http.StatusOK {
		r.t.Fatalf("%s %s: HTTP %d %v", method, path, code, out)
	}
	return out
}

func (r *rig) children() []int {
	b, _ := os.ReadFile(filepath.Join(r.dir, "children"))
	var pids []int
	for _, f := range strings.Fields(string(b)) {
		n, _ := strconv.Atoi(f)
		pids = append(pids, n)
	}
	return pids
}

func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return err == nil && !strings.Contains(string(b), ") Z ") // zombies are dead
}

func final(dialog, text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(finalLine, `"d1"`, `"`+dialog+`"`), "播放晴天", text) + "\n"
}

type sink struct {
	mu     sync.Mutex
	events []Event
	auth   []string
}

func (s *sink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var ev Event
	_ = json.NewDecoder(r.Body).Decode(&ev)
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()
}

func (s *sink) got() ([]Event, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...), append([]string(nil), s.auth...)
}

func TestAuthRequiredExceptHealthz(t *testing.T) {
	r := startRig(t, t.TempDir())
	if code, _ := r.call("GET", "/healthz", "", "", ""); code != 200 {
		t.Fatalf("healthz: %d", code)
	}
	for _, p := range []string{"/v1/status", "/v1/config", "/v1/asr/latest", "/v1/mcp.json"} {
		if code, _ := r.call("GET", p, "", "", ""); code != 401 {
			t.Errorf("%s without auth: %d", p, code)
		}
		if code, _ := r.call("GET", p, DefaultUsername, "wrong-pass", ""); code != 401 {
			t.Errorf("%s with wrong password: %d", p, code)
		}
	}
	if code, _ := r.call("POST", "/mcp", "", "", `{}`); code != 401 {
		t.Errorf("/mcp without auth: %d", code)
	}
}

func TestConfigViaAPIPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s := &sink{}
	hook := httptest.NewServer(s)
	defer hook.Close()

	r := startRig(t, dir)
	cfg := r.admin("GET", "/v1/config", "")["config"].(map[string]any)
	if cfg["auth"].(map[string]any)["default_credentials"] != true || cfg["device"] != "lx06" {
		t.Fatalf("initial config = %v", cfg)
	}
	r.admin("PUT", "/v1/config", `{"device":"客厅","push":{"url":"`+hook.URL+`","token":"pt"},"auth":{"password":"s3cret-pass"}}`)
	if code, _ := r.call("GET", "/v1/status", DefaultUsername, DefaultPassword, ""); code != 401 {
		t.Fatalf("old password still accepted: %d", code)
	}
	code, out := r.call("GET", "/v1/config", DefaultUsername, "s3cret-pass", "")
	if code != 200 {
		t.Fatalf("new password rejected: %d", code)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "s3cret-pass") || strings.Contains(string(raw), `"pt"`) {
		t.Fatalf("GET /v1/config leaks secrets: %s", raw)
	}
	if fi, err := os.Stat(r.cfg); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config file: %v %v", fi, err)
	}
	r.stop()

	// Restart: everything comes back from the file.
	r2 := startRig(t, dir)
	if code, _ := r2.call("GET", "/v1/status", DefaultUsername, "s3cret-pass", ""); code != 200 {
		t.Fatalf("persisted password rejected after restart: %d", code)
	}
	appendFile(t, r2.logf, partialLine+"\n"+speakLine+"\n"+final("d2", "来点陈奕迅的歌"))
	waitFor(t, "push", func() bool { ev, _ := s.got(); return len(ev) == 1 })
	time.Sleep(30 * time.Millisecond) // a partial or duplicate would arrive by now
	ev, auth := s.got()
	if len(ev) != 1 || ev[0].Text != "来点陈奕迅的歌" || ev[0].Device != "客厅" || !ev[0].Final || ev[0].Seq == 0 || auth[0] != "Bearer pt" {
		t.Fatalf("pushed %+v auth %q", ev, auth)
	}
}

func TestConfigRejectsInvalidPatchWithoutWriting(t *testing.T) {
	r := startRig(t, t.TempDir())
	for _, body := range []string{
		`{"push":{"uri":"http://x"}}`,       // typo: unknown field
		`{"push":{"url":"ftp://nas/hook"}}`, // not http(s)
		`{"auth":{"password":"123"}}`,       // too short
		`{"auth":{"username":"a:b"}}`,       // breaks Basic auth
		`{"device":"  "}`,                   // empty
	} {
		if code, _ := r.call("PUT", "/v1/config", DefaultUsername, DefaultPassword, body); code != 400 {
			t.Errorf("PUT %s: %d, want 400", body, code)
		}
	}
	if _, err := os.Stat(r.cfg); !os.IsNotExist(err) {
		t.Fatalf("config file written by rejected patches: %v", err)
	}
}

func TestOpenStoreRefusesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"auth":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path, "x"); err == nil {
		t.Fatal("corrupt config silently replaced by defaults")
	}
}

func TestSpeakInterruptsPreviousAndStopKillsPlayer(t *testing.T) {
	r := startRig(t, t.TempDir())
	r.admin("POST", "/v1/tts", `{"text":"第一段很长的回答"}`)
	waitFor(t, "first player", func() bool { return len(r.children()) == 1 })
	first := r.children()[0]

	r.admin("POST", "/v1/tts", `{"text":"第二段"}`)
	waitFor(t, "second player", func() bool { return len(r.children()) == 2 })
	waitFor(t, "first player killed", func() bool { return !alive(first) })
	st := r.admin("GET", "/v1/status", "")["status"].(map[string]any)
	if tts := st["tts"].(map[string]any); tts["speaking"] != true || tts["text"] != "第二段" {
		t.Fatalf("tts status = %v", tts)
	}

	if out := r.admin("POST", "/v1/stop", ""); out["interrupted"] != true {
		t.Fatalf("stop = %v", out)
	}
	waitFor(t, "second player killed", func() bool { return !alive(r.children()[1]) })
	if out := r.admin("POST", "/v1/stop", ""); out["interrupted"] != false {
		t.Fatalf("second stop = %v", out)
	}
	calls, _ := os.ReadFile(filepath.Join(r.dir, "calls.log"))
	for _, want := range []string{"mphelper\npause\n", "player_play_operation\n{\"action\":\"stop\"}\n"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("stop did not run %q; calls:\n%s", want, calls)
		}
	}
}

func TestSpeakWaitReportsInterruptionByNewerSpeak(t *testing.T) {
	r := startRig(t, t.TempDir())
	res := make(chan map[string]any, 1)
	go func() { res <- r.admin("POST", "/v1/tts", `{"text":"等我说完","wait":true}`) }()
	waitFor(t, "player", func() bool { return len(r.children()) == 1 })
	r.admin("POST", "/v1/tts", `{"text":"插话"}`)
	select {
	case out := <-res:
		if out["interrupted"] != true {
			t.Fatalf("waiting speak = %v", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting speak did not return")
	}
}

func TestSpeakPassesTextVerbatimWithoutShell(t *testing.T) {
	r := startRig(t, t.TempDir())
	text := `它说"你好"'; reboot; $(id) ` + "`id`"
	r.admin("POST", "/v1/tts", `{"text":`+strconv.Quote(text)+`}`)
	waitFor(t, "tts", func() bool { b, _ := os.ReadFile(filepath.Join(r.dir, "tts.log")); return len(b) > 0 })
	b, _ := os.ReadFile(filepath.Join(r.dir, "tts.log"))
	if got := strings.TrimSuffix(string(b), "\n"); got != text {
		t.Fatalf("tts_play.sh got %q, want %q", got, text)
	}
	if code, _ := r.call("POST", "/v1/tts", DefaultUsername, DefaultPassword, `{"text":"  "}`); code != 400 {
		t.Fatalf("empty text: %d", code)
	}
}

// sse reads "event: asr" payloads from a stream.
func sse(t *testing.T, r *rig, query string) (<-chan Event, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", r.base+"/v1/asr/stream"+query, nil)
	req.SetBasicAuth(DefaultUsername, DefaultPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || ct != "text/event-stream" {
		t.Fatalf("stream: %d %s", resp.StatusCode, ct)
	}
	ch := make(chan Event, 16)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var ev Event
				_ = json.Unmarshal([]byte(data), &ev)
				ch <- ev
			}
		}
	}()
	return ch, cancel
}

func next(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no stream event")
	}
	return Event{}
}

func TestStreamDeliversUserInput(t *testing.T) {
	r := startRig(t, t.TempDir())
	finals, stop1 := sse(t, r, "")
	defer stop1()
	all, stop2 := sse(t, r, "?partial=1")
	defer stop2()
	waitFor(t, "subscribers", func() bool {
		st := r.admin("GET", "/v1/status", "")["status"].(map[string]any)
		return st["asr"].(map[string]any)["stream_clients"] == float64(2)
	})

	appendFile(t, r.logf, partialLine+"\n"+finalLine+"\n")
	if ev := next(t, finals); !ev.Final || ev.Text != "播放晴天" {
		t.Fatalf("final stream got %+v", ev)
	}
	if ev := next(t, all); ev.Final || ev.Text != "播放" {
		t.Fatalf("partial stream first got %+v", ev)
	}
	if ev := next(t, all); !ev.Final {
		t.Fatalf("partial stream second got %+v", ev)
	}

	// A late subscriber can ask for the latest input first.
	late, stop3 := sse(t, r, "?last=1")
	defer stop3()
	if ev := next(t, late); ev.Text != "播放晴天" {
		t.Fatalf("last=1 replay got %+v", ev)
	}
	latest := r.admin("GET", "/v1/asr/latest", "")["input"].(map[string]any)
	if latest["text"] != "播放晴天" {
		t.Fatalf("latest = %v", latest)
	}
}

type basicAuthRT struct{ user, pass string }

func (b basicAuthRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth(b.user, b.pass)
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCPToolsOverHTTP(t *testing.T) {
	r := startRig(t, t.TempDir())
	ctx := context.Background()
	tr := &mcp.StreamableClientTransport{Endpoint: r.base + "/mcp", HTTPClient: &http.Client{Transport: basicAuthRT{DefaultUsername, DefaultPassword}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range tools.Tools {
		names = append(names, tl.Name)
	}
	for _, want := range []string{"speak", "stop", "wait_for_input", "get_latest_input", "get_status"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Errorf("tool %s missing from %v", want, names)
		}
	}

	type waitRes struct {
		Input    *Event `json:"input"`
		TimedOut bool   `json:"timed_out"`
	}
	call := func(name string, args any) *mcp.CallToolResult {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}
	decodeWait := func(res *mcp.CallToolResult) waitRes {
		var w waitRes
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &w)
		return w
	}

	got := make(chan waitRes, 1)
	go func() { got <- decodeWait(call("wait_for_input", map[string]any{"timeout_seconds": 5})) }()
	time.Sleep(50 * time.Millisecond)
	appendFile(t, r.logf, final("d7", "今天天气怎么样"))
	w := <-got
	if w.TimedOut || w.Input == nil || w.Input.Text != "今天天气怎么样" {
		t.Fatalf("wait_for_input = %+v", w)
	}
	if w2 := decodeWait(call("wait_for_input", map[string]any{"after_seq": w.Input.Seq, "timeout_seconds": 1})); !w2.TimedOut {
		t.Fatalf("handled input returned again: %+v", w2)
	}

	if res := call("speak", map[string]any{"text": "晴"}); res.IsError {
		t.Fatalf("speak: %+v", res)
	}
	waitFor(t, "tts via mcp", func() bool { b, _ := os.ReadFile(filepath.Join(r.dir, "tts.log")); return string(b) == "晴\n" })
	if res := call("speak", map[string]any{"text": ""}); !res.IsError {
		t.Fatal("empty speak accepted")
	}
}

func TestMCPDescriptor(t *testing.T) {
	r := startRig(t, t.TempDir())
	out := r.admin("GET", "/v1/mcp.json", "")
	srv := out["mcpServers"].(map[string]any)["xiaomi-bridge"].(map[string]any)
	if srv["url"] != r.base+"/mcp" {
		t.Fatalf("url = %v", srv["url"])
	}
	req, _ := http.NewRequest("GET", r.base+"/v1/status", nil)
	req.Header.Set("Authorization", srv["headers"].(map[string]any)["Authorization"].(string))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("descriptor Authorization header does not authenticate: %d", resp.StatusCode)
	}
	for _, tl := range out["tools"].([]any) {
		m := tl.(map[string]any)
		if m["name"] == "speak" {
			schema, _ := json.Marshal(m["inputSchema"])
			if !strings.Contains(string(schema), `"text"`) || m["http"] != "POST /v1/tts" {
				t.Fatalf("speak descriptor = %v", m)
			}
			return
		}
	}
	t.Fatal("speak missing from descriptor")
}

func TestStatusReportsInternalState(t *testing.T) {
	r := startRig(t, t.TempDir())
	appendFile(t, r.logf, speakLine+"\n"+finalLine+"\n")
	waitFor(t, "final", func() bool {
		in, _ := r.admin("GET", "/v1/asr/latest", "")["input"].(map[string]any)
		return in != nil
	})
	st := r.admin("GET", "/v1/status", "")["status"].(map[string]any)
	log := st["instruction_log"].(map[string]any)
	if log["exists"] != true || log["lines"] != float64(2) || st["player"] != "playing" || st["default_credentials"] != true {
		t.Fatalf("status = %v", st)
	}
	if asr := st["asr"].(map[string]any); asr["finals"] != float64(1) {
		t.Fatalf("asr = %v", asr)
	}
	if push := st["push"].(map[string]any); push["enabled"] != false {
		t.Fatalf("push = %v", push)
	}
}
