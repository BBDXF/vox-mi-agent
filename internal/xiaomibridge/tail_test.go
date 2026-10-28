package xiaomibridge

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
