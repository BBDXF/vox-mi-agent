// Package shell implements the run_shell built-in tool (docs/DESIGN.md §6.10).
// There is deliberately no sandbox: the process runs with the app's own
// privileges; only a timeout and output cap apply.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

const (
	DefaultTimeout = 60
	MaxTimeout     = 600
	MaxOutput      = 32 << 10
)

// Deps configures the tool.
type Deps struct {
	Workdir string          // created on demand
	Env     func() []string // child environment (runtimes.ChildEnv)
}

type runShell struct{ d Deps }

// New returns the run_shell tool.
func New(d Deps) tools.Tool { return &runShell{d: d} }

func (*runShell) Name() string { return "run_shell" }
func (*runShell) Description() string {
	return "Run a bash command for system tasks (files, curl, jq, yq, and any installed node/bun/uv). " +
		"Runs in the workspace directory; returns exit_code and combined stdout/stderr (tail kept if long). " +
		"Default timeout 60s (max 600)."
}
func (*runShell) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "minLength": 1, "description": "Shell command line, run with bash -lc."},
    "timeout_sec": {"type": "integer", "minimum": 1, "maximum": 600, "description": "Timeout in seconds (default 60)."}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
}

// tailBuf keeps the last max bytes written and counts what was dropped.
type tailBuf struct {
	max     int
	buf     []byte
	dropped int
}

func (b *tailBuf) Write(p []byte) (int, error) {
	n := len(p)
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.max; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
		b.dropped += over
	}
	return n, nil
}

func (t *runShell) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Command    string `json:"command"`
		TimeoutSec int    `json:"timeout_sec"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return map[string]any{"error": fmt.Sprintf("invalid arguments: %v", err)}, nil
	}
	if a.Command == "" {
		return map[string]any{"error": "command is required"}, nil
	}
	if a.TimeoutSec <= 0 {
		a.TimeoutSec = DefaultTimeout
	}
	if a.TimeoutSec > MaxTimeout {
		a.TimeoutSec = MaxTimeout
	}
	if t.d.Workdir != "" {
		if err := os.MkdirAll(t.d.Workdir, 0o755); err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
	}
	env := os.Environ()
	if t.d.Env != nil {
		env = t.d.Env()
	}
	// Login shells (Alpine /etc/profile) reset PATH: restore the child PATH first.
	pathOf := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			pathOf = v
		}
	}
	var cmd *exec.Cmd
	if p, err := exec.LookPath("bash"); err == nil {
		cmd = exec.Command(p, "-lc", "export PATH=\"$VOX_CHILD_PATH\"\n"+a.Command)
	} else {
		cmd = exec.Command("sh", "-c", a.Command)
	}
	cmd.Dir = t.d.Workdir
	cmd.Env = append(env, "VOX_CHILD_PATH="+pathOf)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out := &tailBuf{max: MaxOutput}
	cmd.Stdout, cmd.Stderr = out, out
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(time.Duration(a.TimeoutSec) * time.Second)
	defer timer.Stop()
	var werr error
	timedOut := false
	select {
	case werr = <-done:
	case <-timer.C:
		timedOut = true
	case <-ctx.Done():
	}
	if werr == nil && (timedOut || ctx.Err() != nil) {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		werr = <-done
	}
	code := 0
	if werr != nil {
		code = -1
		if ee, ok := werr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	// reap stragglers left in the group by a normally exiting shell
	if !timedOut && ctx.Err() == nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	res := map[string]any{
		"exit_code":   code,
		"output":      string(bytes.ToValidUTF8(out.buf, []byte("?"))),
		"truncated":   out.dropped > 0,
		"timed_out":   timedOut,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	if timedOut {
		res["error"] = fmt.Sprintf("timed out after %ds", a.TimeoutSec)
	}
	if ctx.Err() != nil {
		res["error"] = "cancelled"
	}
	return res, nil
}
