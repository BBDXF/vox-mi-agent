package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestContextAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := New("info", &buf)
	ctx := WithTurnID(WithRequestID(context.Background(), "r1"), "t1")
	l.InfoContext(ctx, "hello", "k", "v")
	l.With("a", 1).InfoContext(context.Background(), "plain")
	l.DebugContext(ctx, "dropped")

	dec := json.NewDecoder(&buf)
	var m1, m2 map[string]any
	if err := dec.Decode(&m1); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&m2); err != nil {
		t.Fatal(err)
	}
	if m1["request_id"] != "r1" || m1["turn_id"] != "t1" || m1["k"] != "v" || m1["msg"] != "hello" {
		t.Errorf("m1=%v", m1)
	}
	if _, ok := m2["request_id"]; ok || m2["a"] != float64(1) {
		t.Errorf("m2=%v", m2)
	}
	if dec.More() {
		t.Error("debug record should be filtered")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "WARN": slog.LevelWarn, "error": slog.LevelError, "": slog.LevelInfo, "x": slog.LevelInfo} {
		if got := ParseLevel(in); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
	if len(NewID()) != 12 {
		t.Error("NewID")
	}
}
