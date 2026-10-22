package shell

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func call(t *testing.T, ctx context.Context, d Deps, args string) map[string]any {
	t.Helper()
	r, err := New(d).Call(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return r.(map[string]any)
}

func TestRunShell(t *testing.T) {
	d := Deps{Workdir: t.TempDir() + "/ws", Env: func() []string { return []string{"PATH=/usr/bin:/bin", "FOO=bar"} }}
	r := call(t, context.Background(), d, `{"command":"echo hi $FOO; pwd; echo err >&2; exit 3"}`)
	if r["exit_code"] != 3 {
		t.Fatalf("%v", r)
	}
	out := r["output"].(string)
	if !strings.Contains(out, "hi bar") || !strings.Contains(out, "err") || !strings.Contains(out, "/ws") {
		t.Fatalf("output %q", out)
	}
	if call(t, context.Background(), d, `{"command":"true"}`)["exit_code"] != 0 {
		t.Fatal("exit 0 expected")
	}
}

func TestRunShellTimeout(t *testing.T) {
	d := Deps{Workdir: t.TempDir()}
	start := time.Now()
	r := call(t, context.Background(), d, `{"command":"sleep 30 & sleep 30","timeout_sec":1}`)
	if r["timed_out"] != true || time.Since(start) > 10*time.Second {
		t.Fatalf("%v", r)
	}
}

func TestRunShellCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r := call(t, ctx, Deps{Workdir: t.TempDir()}, `{"command":"sleep 30"}`)
	if r["error"] != "cancelled" {
		t.Fatalf("%v", r)
	}
}

func TestRunShellTruncate(t *testing.T) {
	r := call(t, context.Background(), Deps{Workdir: t.TempDir()}, `{"command":"head -c 100000 /dev/zero | tr '\\0' a; echo END"}`)
	out := r["output"].(string)
	if r["truncated"] != true || len(out) != MaxOutput || !strings.HasSuffix(out, "END\n") {
		t.Fatalf("len=%d %v", len(out), r["truncated"])
	}
}

func TestRunShellBadArgs(t *testing.T) {
	if call(t, context.Background(), Deps{}, `{}`)["error"] == nil {
		t.Fatal("expected error")
	}
}
