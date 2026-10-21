package toolrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func toolPath(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("VOX_TOOLS_DIR")
	if dir == "" {
		dir, _ = filepath.Abs("../../.tools")
	}
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s missing", p)
	}
	return p
}

func newRunner(t *testing.T) *Runner {
	r := New()
	r.WorkDir = filepath.Join(t.TempDir(), "toolrun") // must be created on demand
	return r
}

func TestRunBasicAndEnv(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	var recs []Record
	r.Auditor = func(rc Record) { recs = append(recs, rc) }
	t.Setenv("VOX_SECRET_TEST", "x")
	res, err := r.Run(context.Background(), Spec{
		Tool: "query_json", Source: "s", Query: "$ENV|keys", Path: jq,
		Args: []string{"-c", "--", "$ENV|keys"}, Stdin: []byte("null"),
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != `["LANG","PATH"]` {
		t.Fatalf("env = %s", got)
	}
	if _, err := os.Stat(r.WorkDir); err != nil {
		t.Fatal("workdir not created")
	}
	if len(recs) != 1 || recs[0].Tool != "query_json" || recs[0].Source != "s" ||
		recs[0].Query != "$ENV|keys" || recs[0].OutputBytes != len(res.Stdout) || recs[0].Duration <= 0 {
		t.Fatalf("audit: %+v", recs)
	}
}

func TestRunExitCodeAndStderr(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	res, err := r.Run(context.Background(), Spec{Path: jq, Args: []string{"--", ".["}, Stdin: []byte("1")})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 || len(res.Stderr) == 0 {
		t.Fatalf("%+v", res)
	}
	r.MaxStderr = 10
	res, _ = r.Run(context.Background(), Spec{Path: jq, Args: []string{"--", ".["}, Stdin: []byte("1")})
	if len(res.Stderr) > 10 {
		t.Fatalf("stderr not truncated: %d", len(res.Stderr))
	}
}

func TestRunValidation(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	r.MaxStdin = 10
	if _, err := r.Run(context.Background(), Spec{Path: jq, Args: []string{"."}, Stdin: make([]byte, 11)}); !errors.Is(err, ErrStdinTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.Run(context.Background(), Spec{Path: "jq", Args: []string{"."}}); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestRunStdoutTruncation(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	r.MaxStdout = 100
	res, err := r.Run(context.Background(), Spec{Path: jq, Args: []string{"-n", "range(10000)"}})
	if err != nil || !res.Truncated || len(res.Stdout) != 100 {
		t.Fatalf("%v trunc=%v len=%d", err, res.Truncated, len(res.Stdout))
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	start := time.Now()
	res, err := r.Run(context.Background(), Spec{
		Path: jq, Args: []string{"-n", "def f: f; f"}, Timeout: 300 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	_ = res
}

// A shell script is used only in this test to create a grandchild process and
// verify the whole process group is killed.
func TestRunKillsGrandchildren(t *testing.T) {
	r := newRunner(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "spawn.sh")
	os.WriteFile(script, []byte("#!/bin/sh\n/bin/sleep 30 &\necho $! > "+pidFile+"\nwait\n"), 0o755)
	_, err := r.Run(context.Background(), Spec{Path: script, Timeout: 500 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Skip("script did not run (no /bin/sh?)")
	}
	pid := strings.TrimSpace(string(b))
	time.Sleep(200 * time.Millisecond)
	if st, err := os.ReadFile("/proc/" + pid + "/stat"); err == nil && !strings.Contains(string(st), ") Z") {
		t.Fatalf("grandchild %s still alive: %s", pid, st)
	}
}

func TestRunContextCancel(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err := r.Run(ctx, Spec{Path: jq, Args: []string{"-n", "def f: f; f"}, Timeout: 10 * time.Second})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunConcurrencyLimit(t *testing.T) {
	jq := toolPath(t, "jq")
	r := newRunner(t)
	r.Concurrency = 2
	// Measure overlap via wall time: 4 jobs of ~300ms timeouts with limit 2 => >= ~600ms.
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Run(context.Background(), Spec{Path: jq, Args: []string{"-n", "def f: f; f"}, Timeout: 300 * time.Millisecond})
		}()
	}
	wg.Wait()
	if d := time.Since(start); d < 550*time.Millisecond {
		t.Fatalf("concurrency not limited: %v", d)
	}
}
