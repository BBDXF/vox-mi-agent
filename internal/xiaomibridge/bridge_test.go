package xiaomibridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

type lines struct {
	mu sync.Mutex
	l  []string
}

func (c *lines) add(b []byte) { c.mu.Lock(); c.l = append(c.l, string(b)); c.mu.Unlock() }
func (c *lines) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.l...)
}

func TestTailerSkipsHistoryAndSurvivesTruncateAndReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instruction.log")
	appendFile(t, path, "old-1\nold-2\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got lines
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = (&Tailer{Path: path, Every: time.Millisecond}).Run(ctx, got.add)
	}()
	time.Sleep(20 * time.Millisecond) // let it open the file at its end

	appendFile(t, path, "a\npart")
	waitFor(t, "a", func() bool { return len(got.get()) == 1 })
	appendFile(t, path, "ial\r\n")
	waitFor(t, "partial completed", func() bool { return len(got.get()) == 2 })

	if err := os.Truncate(path, 0); err != nil { // logrotate copytruncate
		t.Fatal(err)
	}
	appendFile(t, path, "b\n")
	waitFor(t, "b after truncate", func() bool { return len(got.get()) == 3 })

	tmp := filepath.Join(dir, "new.log") // service restart writes a new file
	appendFile(t, tmp, "c\n")
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "c after replace", func() bool { return len(got.get()) == 4 })

	cancel()
	<-done
	want := []string{"a", "partial", "b", "c"}
	if g := got.get(); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("lines = %q, want %q", g, want)
	}
}

func TestTailerWaitsForMissingFileAndReadsItFromStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instruction.log")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got lines
	go func() { _ = (&Tailer{Path: path, Every: time.Millisecond}).Run(ctx, got.add) }()
	time.Sleep(10 * time.Millisecond)
	appendFile(t, path, "first\n") // mico_aivs_lab creates the log after the bridge started
	waitFor(t, "first", func() bool { return len(got.get()) == 1 && got.get()[0] == "first" })
}

const (
	finalLine   = `{"header":{"dialog_id":"d1","id":"i1","name":"RecognizeResult","namespace":"SpeechRecognizer"},"payload":{"is_final":true,"is_vad_begin":false,"results":[{"confidence":0.9,"text":"播放晴天"}]}}`
	partialLine = `{"header":{"dialog_id":"d1","id":"i0","name":"RecognizeResult","namespace":"SpeechRecognizer"},"payload":{"is_final":false,"is_vad_begin":true,"results":[{"text":"播放"}]}}`
	speakLine   = `{"header":{"dialog_id":"d1","id":"i2","name":"Speak","namespace":"SpeechSynthesizer"},"payload":{"text":"好的"}}`
)

func TestParseRecognition(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Recognition
		ok   bool
	}{
		{"final", finalLine, Recognition{DialogID: "d1", Text: "播放晴天", Final: true}, true},
		{"partial", partialLine, Recognition{DialogID: "d1", Text: "播放"}, true},
		{"log prefix", "[2026-10-28 12:00:00] " + finalLine, Recognition{DialogID: "d1", Text: "播放晴天", Final: true}, true},
		{"other directive", speakLine, Recognition{}, false},
		{"empty text", `{"header":{"name":"RecognizeResult","namespace":"SpeechRecognizer"},"payload":{"is_final":true,"results":[{"text":"  "}]}}`, Recognition{}, false},
		{"no results", `{"header":{"name":"RecognizeResult","namespace":"SpeechRecognizer"},"payload":{"is_final":true,"results":[]}}`, Recognition{}, false},
		{"garbage", "not json", Recognition{}, false},
	}
	for _, c := range cases {
		got, ok := ParseRecognition([]byte(c.line))
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
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

func runBridge(t *testing.T, c Config) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	time.Sleep(20 * time.Millisecond) // tail starts at the current end
	return cancel
}

func TestRunPushesFinalRecognitionOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instruction.log")
	appendFile(t, path, finalLine+"\n") // history before start: not pushed
	s := &sink{}
	srv := httptest.NewServer(s)
	defer srv.Close()
	runBridge(t, Config{LogPath: path, Poll: time.Millisecond, Device: "lx06", PushURL: srv.URL, PushToken: "pt", Log: quietLog(),
		Now: func() time.Time { return time.UnixMilli(42) }})

	final2 := strings.ReplaceAll(strings.ReplaceAll(finalLine, "d1", "d2"), "播放晴天", "来点陈奕迅的歌")
	appendFile(t, path, partialLine+"\n"+speakLine+"\n"+final2+"\n"+final2+"\n")
	waitFor(t, "push", func() bool { ev, _ := s.got(); return len(ev) >= 1 })
	time.Sleep(30 * time.Millisecond) // a duplicate would arrive by now

	ev, auth := s.got()
	want := Event{Type: "asr", Device: "lx06", DialogID: "d2", Text: "来点陈奕迅的歌", Final: true, TS: 42}
	if len(ev) != 1 || ev[0] != want {
		t.Fatalf("events = %+v, want [%+v]", ev, want)
	}
	if auth[0] != "Bearer pt" {
		t.Fatalf("auth = %q", auth[0])
	}
}

func TestRunPushesPartialsWhenEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instruction.log")
	appendFile(t, path, "")
	s := &sink{}
	srv := httptest.NewServer(s)
	defer srv.Close()
	runBridge(t, Config{LogPath: path, Poll: time.Millisecond, PushURL: srv.URL, Partial: true, Log: quietLog()})

	appendFile(t, path, partialLine+"\n"+partialLine+"\n"+finalLine+"\n")
	waitFor(t, "two pushes", func() bool { ev, _ := s.got(); return len(ev) >= 2 })
	time.Sleep(30 * time.Millisecond)
	ev, auth := s.got()
	if len(ev) != 2 || ev[0].Final || ev[0].Text != "播放" || !ev[1].Final || ev[1].Text != "播放晴天" {
		t.Fatalf("events = %+v", ev)
	}
	if auth[0] != "" {
		t.Fatalf("no push token configured, got auth %q", auth[0])
	}
}

func TestRunRejectsUnsafeConfig(t *testing.T) {
	if err := Run(context.Background(), Config{}); err == nil {
		t.Fatal("empty config accepted")
	}
	if err := Run(context.Background(), Config{Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("control API without token accepted")
	}
}

// fakeDevice installs scripts that record their arguments (one per line).
func fakeDevice(t *testing.T) (*Device, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls.log")
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	rec := `{ echo "$0"; for a in "$@"; do echo "$a"; done; echo --; } >> ` + calls + "\n"
	d := &Device{
		Ubus:     write("ubus", rec+`echo '{"code": 0}'`),
		Mphelper: write("mphelper", rec+`[ "$1" = mute_stat ] && echo 1; exit 0`),
		// Like the real tts_play.sh: a player child does the playing.
		TTSPlay:  write("tts_play.sh", rec+"sleep 30 &\necho $! > "+filepath.Join(dir, "child.pid")+"\nwait\n"),
		AivsInit: write("mico_aivs_lab", rec),
	}
	return d, dir
}

func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return err == nil && !strings.Contains(string(b), ") Z ") // zombies are dead
}

func TestStopKillsBlockingTTSIncludingPlayerChild(t *testing.T) {
	d, dir := fakeDevice(t)
	h := ControlHandler(d, "tok", quietLog())
	srv := httptest.NewServer(h)
	defer srv.Close()
	post := func(path, body string) map[string]any {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: HTTP %d %v", path, resp.StatusCode, out)
		}
		return out
	}

	res := make(chan map[string]any, 1)
	go func() { res <- post("/v1/tts", `{"text":"这是一段很长的回答","mode":"blocking"}`) }()
	pidFile := filepath.Join(dir, "child.pid")
	waitFor(t, "player child", func() bool { b, _ := os.ReadFile(pidFile); return len(bytes.TrimSpace(b)) > 0 })
	b, _ := os.ReadFile(pidFile)
	child, _ := strconv.Atoi(strings.TrimSpace(string(b)))

	stop := post("/v1/stop", "")
	if stop["interrupted"] != true {
		t.Fatalf("stop = %v", stop)
	}
	select {
	case r := <-res:
		if r["ok"] != true || r["interrupted"] != true {
			t.Fatalf("blocking tts = %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocking tts did not return after stop")
	}
	waitFor(t, "player child killed", func() bool { return !alive(child) })

	calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	for _, want := range []string{"mphelper\npause\n", "player_play_operation\n{\"action\":\"stop\"}\n"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("stop did not run %q; calls:\n%s", want, calls)
		}
	}
	if again := post("/v1/stop", ""); again["interrupted"] != false {
		t.Fatalf("second stop = %v, want interrupted=false", again)
	}
}

func TestSpeakPassesTextVerbatimWithoutShell(t *testing.T) {
	d, dir := fakeDevice(t)
	text := `它说"你好"'; reboot; $(id)`
	if err := d.Speak(context.Background(), text); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	parts := strings.Split(strings.TrimSuffix(string(calls), "--\n"), "\n")
	var msg map[string]any
	if err := json.Unmarshal([]byte(parts[4]), &msg); err != nil || msg["text"] != text {
		t.Fatalf("ubus message arg = %q (%v)", parts[4], err)
	}
}

func TestControlAuthAndValidation(t *testing.T) {
	d, _ := fakeDevice(t)
	srv := httptest.NewServer(ControlHandler(d, "tok", quietLog()))
	defer srv.Close()
	do := func(method, path, auth, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	cases := []struct {
		method, path, auth, body string
		want                     int
	}{
		{"GET", "/healthz", "", "", 200},
		{"POST", "/v1/stop", "", "", 401},
		{"POST", "/v1/stop", "Bearer wrong", "", 401},
		{"POST", "/v1/abort-xiaoai", "tok", "", 401}, // scheme required
		{"GET", "/v1/status", "Bearer tok", "", 200},
		{"POST", "/v1/tts", "Bearer tok", `{"text":""}`, 400},
		{"POST", "/v1/tts", "Bearer tok", `{"text":"hi","mode":"loud"}`, 400},
		{"POST", "/v1/play-url", "Bearer tok", `{"url":"file:///etc/passwd"}`, 400},
		{"POST", "/v1/play-url", "Bearer tok", `{"url":"http://nas:8080/audio/x.mp3"}`, 200},
	}
	for _, c := range cases {
		if got := do(c.method, c.path, c.auth, c.body); got != c.want {
			t.Errorf("%s %s auth=%q body=%s: %d, want %d", c.method, c.path, c.auth, c.body, got, c.want)
		}
	}
}
