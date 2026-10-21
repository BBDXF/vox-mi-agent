package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/store"
)

type countSink struct {
	mu      sync.Mutex
	batches []int
	rows    int
	block   chan struct{} // when non-nil, InsertTraces waits on it
	fail    bool
}

func (c *countSink) InsertTraces(ctx context.Context, rows []store.TraceRow) error {
	if c.block != nil {
		<-c.block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("disk full")
	}
	c.batches = append(c.batches, len(rows))
	c.rows += len(rows)
	return nil
}

func (c *countSink) snapshot() ([]int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.batches...), c.rows
}

func TestWriterBatchingFlushAndClose(t *testing.T) {
	sink := &countSink{}
	w := NewWriter(sink, nil, WriterOptions{Buffer: 1000, Batch: 100, Interval: time.Hour})
	for i := 0; i < 250; i++ {
		if !w.Add(store.TraceRow{Kind: store.TraceTool, Status: store.TraceOK}) {
			t.Fatal("unexpected drop")
		}
	}
	w.Flush()
	b, n := sink.snapshot()
	if n != 250 || w.Written() != 250 {
		t.Fatalf("written %d batches %v", n, b)
	}
	for _, sz := range b {
		if sz > 100 {
			t.Fatalf("batch too large: %v", b)
		}
	}
	if len(b) < 3 {
		t.Fatalf("expected >=3 batches, got %v", b)
	}
	// Close flushes what is still buffered (the ticker never fires here)
	for i := 0; i < 7; i++ {
		w.Add(store.TraceRow{Kind: store.TraceTurn, Status: store.TraceOK})
	}
	w.Close()
	if _, n := sink.snapshot(); n != 257 {
		t.Fatalf("after close: %d", n)
	}
	if w.Add(store.TraceRow{}) || w.Dropped() != 1 {
		t.Fatalf("add after close must drop (dropped=%d)", w.Dropped())
	}
	w.Close() // idempotent
}

func TestWriterTimedFlush(t *testing.T) {
	sink := &countSink{}
	w := NewWriter(sink, nil, WriterOptions{Interval: 10 * time.Millisecond})
	defer w.Close()
	w.Add(store.TraceRow{})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, n := sink.snapshot(); n == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("row not flushed by the ticker")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWriterDropsWhenFull(t *testing.T) {
	sink := &countSink{block: make(chan struct{})}
	var logbuf bytes.Buffer
	lg := slog.New(slog.NewTextHandler(&logbuf, nil))
	w := NewWriter(sink, lg, WriterOptions{Buffer: 4, Batch: 1, Interval: time.Hour})
	// batch=1: the first row is taken by the goroutine and blocks in the sink;
	// 4 more fill the buffer, the rest must be dropped without blocking.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			w.Add(store.TraceRow{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Add blocked")
	}
	if w.Dropped() < 40 {
		t.Fatalf("dropped %d", w.Dropped())
	}
	if !strings.Contains(logbuf.String(), "dropping trace rows") {
		t.Fatalf("no drop warning: %s", logbuf.String())
	}
	close(sink.block)
	w.Close()
	if _, n := sink.snapshot(); n != 50-int(w.Dropped()) {
		t.Fatalf("written %d dropped %d", n, w.Dropped())
	}
}

func TestWriterInsertFailureDoesNotWedge(t *testing.T) {
	sink := &countSink{fail: true}
	w := NewWriter(sink, nil, WriterOptions{Batch: 2})
	w.Add(store.TraceRow{})
	w.Add(store.TraceRow{})
	w.Flush()
	sink.mu.Lock()
	sink.fail = false
	sink.mu.Unlock()
	w.Add(store.TraceRow{})
	w.Flush()
	if _, n := sink.snapshot(); n != 1 {
		t.Fatalf("written %d", n)
	}
	w.Close()
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRecorderRetention(t *testing.T) {
	st := openStore(t)
	rec := NewRecorder(st, t.TempDir(), nil, WriterOptions{})
	defer rec.Close()
	now := time.Now()
	for i := 19; i >= 0; i-- { // oldest first, like real time order
		rec.Trace(store.TraceRow{TS: now.Add(-time.Duration(i) * 24 * time.Hour), Kind: store.TraceTurn, Status: store.TraceOK, TurnID: "t"})
	}
	rec.Flush()
	// 20 rows aged 0..19 days; keep 7 days (age < 7d: i=0..6 -> 7 rows) and at most 5 rows
	rec.Configure(config.Logging{TraceRetentionDays: 7, TraceMaxRows: 5, PayloadRetentionDays: 7, PayloadMaxBytes: 1 << 20})
	n, err := rec.PruneNow(context.Background())
	if err != nil || n != 15 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if c, _ := st.CountTraces(context.Background()); c != 5 {
		t.Fatalf("left %d", c)
	}
	// the survivors are the newest rows
	rows, _ := st.QueryTraces(context.Background(), store.TraceFilter{})
	for _, r := range rows {
		if r.TS.Before(now.Add(-5 * 24 * time.Hour)) {
			t.Fatalf("old row survived: %+v", r)
		}
	}
}

func TestRecorderStartRunsRetention(t *testing.T) {
	st := openStore(t)
	rec := NewRecorder(st, t.TempDir(), nil, WriterOptions{})
	rec.Configure(config.Logging{TraceRetentionDays: 1, TraceMaxRows: 1000, PayloadRetentionDays: 7, PayloadMaxBytes: 1 << 20})
	if err := st.InsertTraces(context.Background(), []store.TraceRow{
		{TS: time.Now().Add(-48 * time.Hour), Kind: store.TraceTurn, Status: store.TraceOK},
		{TS: time.Now(), Kind: store.TraceTurn, Status: store.TraceOK},
	}); err != nil {
		t.Fatal(err)
	}
	rec.Start()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if c, _ := st.CountTraces(context.Background()); c == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup retention did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec.Close()
}

// ---- payload log ----

func newPayload(t *testing.T) (*PayloadLog, string, *time.Time) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "logs")
	p := NewPayloadLog(dir, nil)
	clock := time.Date(2026, 3, 10, 12, 0, 0, 0, time.Local)
	p.now = func() time.Time { return clock }
	p.Configure(true, 7, 10<<20)
	t.Cleanup(p.Close)
	return p, dir, &clock
}

func TestPayloadWriteRotateAndRead(t *testing.T) {
	p, dir, clock := newPayload(t)
	req := &agent.PayloadRequest{Model: "m", Messages: []llm.Message{{Role: "user", Content: "你好"}}}
	p.Write(agent.Payload{TS: *clock, TurnID: "turnaaa", Step: 1, Kind: "llm", Request: req,
		Response: &agent.PayloadResponse{Text: "hi", Usage: &llm.Usage{PromptTokens: 3}}})
	p.Write(agent.Payload{TS: *clock, TurnID: "turnaaa", Step: 1, Kind: "tool", Tool: "echo",
		Args: json.RawMessage(`{"a":1}`), Result: json.RawMessage(`{"full":"result"}`)})
	p.Write(agent.Payload{TS: *clock, TurnID: "turnbbb", Step: 1, Kind: "llm", Request: req})
	p.Flush()
	day1 := filepath.Join(dir, "trace-20260310.jsonl")
	raw, err := os.ReadFile(day1)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "\n"); lines != 3 {
		t.Fatalf("%d lines:\n%s", lines, raw)
	}
	if fi, _ := os.Stat(day1); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}

	// next day -> new file
	*clock = clock.Add(24 * time.Hour)
	p.Write(agent.Payload{TS: *clock, TurnID: "turnaaa", Step: 2, Kind: "llm", Request: req})
	p.Flush()
	if _, err := os.Stat(filepath.Join(dir, "trace-20260311.jsonl")); err != nil {
		t.Fatalf("no rotated file: %v", err)
	}

	items, err := ReadTurn(dir, "turnaaa", time.Date(2026, 3, 10, 12, 0, 0, 0, time.Local), 100)
	if err != nil || len(items) != 3 { // 2 on day 1 + 1 spilled into day 2
		t.Fatalf("%v %d", err, len(items))
	}
	var first struct {
		Kind     string `json:"kind"`
		Step     int    `json:"step"`
		Request  struct{ Model string }
		Response struct{ Text string }
	}
	if err := json.Unmarshal(items[0], &first); err != nil || first.Kind != "llm" || first.Request.Model != "m" || first.Response.Text != "hi" {
		t.Fatalf("%v %+v", err, first)
	}
	if !strings.Contains(string(items[1]), `"full":"result"`) {
		t.Fatalf("tool payload lost the full result: %s", items[1])
	}
	if got, _ := ReadTurn(dir, "nope", time.Time{}, 10); len(got) != 0 {
		t.Fatal("unknown turn returned lines")
	}
	if got, _ := ReadTurn(dir, "turnbbb", time.Date(2026, 3, 10, 0, 0, 0, 0, time.Local), 10); len(got) != 1 {
		t.Fatalf("turnbbb %d", len(got))
	}
}

func TestPayloadDisabledWritesNothingAndNoSecrets(t *testing.T) {
	p, dir, clock := newPayload(t)
	p.Configure(false, 7, 1<<20)
	p.Write(agent.Payload{TS: *clock, TurnID: "x", Kind: "tool", Tool: "t"})
	p.Flush()
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("files written while disabled: %v", ents)
	}
	p.Configure(true, 7, 1<<20)
	const key = "sk-very-secret-key-123456"
	p.SetSecrets(key)
	p.Write(agent.Payload{TS: *clock, TurnID: "x", Kind: "tool", Tool: "t", Args: json.RawMessage(`{"auth":"` + key + `"}`)})
	p.Flush()
	raw, _ := os.ReadFile(filepath.Join(dir, "trace-20260310.jsonl"))
	if len(raw) == 0 || strings.Contains(string(raw), key) || !strings.Contains(string(raw), "***") {
		t.Fatalf("secret leaked or line missing: %s", raw)
	}
}

func TestPayloadPrune(t *testing.T) {
	p, dir, clock := newPayload(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mk := func(name string, size int) {
		if err := os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte("x"), size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// "today" is 2026-03-10; retention 7 days -> files before 03-03 are removed
	mk("trace-20260301.jsonl", 100)
	mk("trace-20260302.jsonl", 100)
	mk("trace-20260303.jsonl", 100)
	mk("trace-20260308.jsonl", 400)
	mk("trace-20260309.jsonl", 400)
	mk("trace-20260310.jsonl", 400)
	mk("unrelated.txt", 10)
	p.Configure(true, 7, 1<<30)
	if n := p.Prune(); n != 2 {
		t.Fatalf("age prune removed %d", n)
	}
	// total size cap 1000: 100+400+400+400=1300 -> drop oldest until <= 1000 (two files)
	p.Configure(true, 7, 1000)
	if n := p.Prune(); n != 2 {
		t.Fatalf("size prune removed %d", n)
	}
	var left []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		left = append(left, e.Name())
	}
	if got := strings.Join(left, ","); got != "trace-20260309.jsonl,trace-20260310.jsonl,unrelated.txt" {
		t.Fatalf("left %s", got)
	}
	// the newest file is never removed even when it alone exceeds the cap
	p.Configure(true, 7, 10)
	p.Prune()
	if _, err := os.Stat(filepath.Join(dir, "trace-20260310.jsonl")); err != nil {
		t.Fatal("newest file removed")
	}
	_ = clock
}

func TestPayloadSizeCapDropsLines(t *testing.T) {
	p, dir, clock := newPayload(t)
	p.Configure(true, 7, 600)
	for i := 0; i < 10; i++ {
		p.Write(agent.Payload{TS: *clock, TurnID: "t", Kind: "tool", Tool: strings.Repeat("a", 100)})
	}
	p.Flush()
	fi, err := os.Stat(filepath.Join(dir, "trace-20260310.jsonl"))
	if err != nil || fi.Size() > 600 {
		t.Fatalf("size %v %v", fi, err)
	}
	if p.Dropped() == 0 {
		t.Fatal("no drops counted")
	}
}

func TestRecorderImplementsTracerAndPayloadToggle(t *testing.T) {
	st := openStore(t)
	dir := t.TempDir()
	rec := NewRecorder(st, dir, nil, WriterOptions{})
	defer rec.Close()
	var _ agent.Tracer = rec
	if rec.PayloadEnabled() {
		t.Fatal("payloads must default to off")
	}
	rec.Configure(config.Logging{DebugPayloads: true, TraceRetentionDays: 30, TraceMaxRows: 1000, PayloadRetentionDays: 7, PayloadMaxBytes: 1 << 20})
	if !rec.PayloadEnabled() {
		t.Fatal("not enabled")
	}
	rec.Payload(agent.Payload{TS: time.Now(), TurnID: "abc", Kind: "tool", Tool: "x"})
	rec.Flush()
	items, _ := ReadTurn(dir, "abc", time.Now(), 10)
	if len(items) != 1 {
		t.Fatalf("%d", len(items))
	}
}
