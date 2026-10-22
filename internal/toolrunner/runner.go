// Package toolrunner is the only place that spawns external tool binaries
// (fzq / jq / yq). See docs/DESIGN.md §6.9.
//
// Rules enforced here: no shell, argv-only; environment cleared to
// LANG/PATH; own process group killed on timeout/cancel; stdin/stdout/stderr
// size limits; bounded concurrency; audit callback per call.
package toolrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Defaults.
const (
	DefaultTimeout     = 3 * time.Second
	DefaultMaxStdin    = 256 << 10
	DefaultMaxStdout   = 64 << 10
	DefaultMaxStderr   = 2 << 10
	DefaultConcurrency = 4
	DefaultWorkDir     = "/tmp/toolrun"
)

var (
	// ErrTimeout is returned (wrapped) when the per-call timeout expired.
	ErrTimeout = errors.New("toolrunner: timeout")
	// ErrStdinTooLarge is returned when Spec.Stdin exceeds Runner.MaxStdin.
	ErrStdinTooLarge = errors.New("toolrunner: stdin too large")
)

// Spec describes one invocation.
type Spec struct {
	Tool    string        // tool name, audit only (e.g. "query_json")
	Source  string        // registered source name, audit only
	Query   string        // query / expression, audit only
	Path    string        // absolute path of the binary
	Args    []string      // argv[1:]
	Stdin   []byte        // may be nil
	Timeout time.Duration // 0 => Runner.Timeout
}

// Result of a finished process. A non-zero ExitCode is NOT a Go error.
type Result struct {
	ExitCode int // -1 if killed by a signal
	Stdout   []byte
	Stderr   []byte
	// Truncated reports that stdout exceeded MaxStdout and was cut.
	Truncated bool
	Duration  time.Duration
}

// Record is passed to Runner.Auditor after every Run. It never contains the
// stdin payload.
type Record struct {
	Tool        string
	Source      string
	Query       string
	ExitCode    int
	Duration    time.Duration
	OutputBytes int // total stdout bytes produced (before truncation)
	Truncated   bool
	Err         string // empty on success
}

// Runner executes external tools. Use New for defaults; zero fields fall back
// to the same defaults. Configure fields before the first Run.
type Runner struct {
	Timeout     time.Duration
	MaxStdin    int
	MaxStdout   int
	MaxStderr   int
	Concurrency int
	WorkDir     string
	Auditor     func(Record)

	once sync.Once
	sem  chan struct{}
}

// New returns a Runner with the §6.9 defaults.
func New() *Runner {
	return &Runner{
		Timeout:     DefaultTimeout,
		MaxStdin:    DefaultMaxStdin,
		MaxStdout:   DefaultMaxStdout,
		MaxStderr:   DefaultMaxStderr,
		Concurrency: DefaultConcurrency,
		WorkDir:     DefaultWorkDir,
	}
}

func orInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func (r *Runner) init() {
	r.once.Do(func() { r.sem = make(chan struct{}, orInt(r.Concurrency, DefaultConcurrency)) })
}

func (r *Runner) workDir() string {
	if r.WorkDir == "" {
		return DefaultWorkDir
	}
	return r.WorkDir
}

// ensureWorkDir creates the work dir if missing.
func (r *Runner) ensureWorkDir() error {
	return os.MkdirAll(r.workDir(), 0o700)
}

// TempFile writes data to a new file in WorkDir (name pattern as os.CreateTemp)
// and returns its absolute path plus a cleanup func.
func (r *Runner) TempFile(pattern string, data []byte) (string, func(), error) {
	if err := r.ensureWorkDir(); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp(r.workDir(), pattern)
	if err != nil {
		return "", nil, err
	}
	name := f.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := f.Write(data); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return abs, cleanup, nil
}

// capBuf keeps at most max bytes, counts everything.
type capBuf struct {
	max       int
	buf       bytes.Buffer
	total     int
	truncated bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.total += len(p)
	room := c.max - c.buf.Len()
	if room > 0 {
		n := len(p)
		if n > room {
			n = room
		}
		c.buf.Write(p[:n])
	}
	if len(p) > room {
		c.truncated = true
	}
	return len(p), nil
}

// bytes returns the kept data, without a trailing partial UTF-8 rune if cut.
func (c *capBuf) bytes() []byte {
	b := c.buf.Bytes()
	if c.truncated {
		for i := 0; i < utf8.UTFMax && len(b) > 0; i++ {
			r, size := utf8.DecodeLastRune(b)
			if r != utf8.RuneError || size != 1 {
				break
			}
			b = b[:len(b)-1]
		}
	}
	return append([]byte(nil), b...)
}

// Run executes spec. A non-zero exit code is reported in Result, not as an
// error. Errors: ErrStdinTooLarge, ErrTimeout (wrapped), ctx errors, or
// start/IO failures.
func (r *Runner) Run(ctx context.Context, spec Spec) (res Result, err error) {
	r.init()
	start := time.Now()
	var out *capBuf
	defer func() {
		res.Duration = time.Since(start)
		if r.Auditor != nil {
			rec := Record{
				Tool: spec.Tool, Source: spec.Source, Query: spec.Query,
				ExitCode: res.ExitCode, Duration: res.Duration, Truncated: res.Truncated,
			}
			if out != nil {
				rec.OutputBytes = out.total
			}
			if err != nil {
				rec.Err = err.Error()
			}
			r.Auditor(rec)
		}
	}()

	if !filepath.IsAbs(spec.Path) {
		return res, fmt.Errorf("toolrunner: path %q is not absolute", spec.Path)
	}
	if max := orInt(r.MaxStdin, DefaultMaxStdin); len(spec.Stdin) > max {
		return res, fmt.Errorf("%w: %d > %d bytes", ErrStdinTooLarge, len(spec.Stdin), max)
	}
	if err := r.ensureWorkDir(); err != nil {
		return res, fmt.Errorf("toolrunner: workdir: %w", err)
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return res, ctx.Err()
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = r.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out = &capBuf{max: orInt(r.MaxStdout, DefaultMaxStdout)}
	errb := &capBuf{max: orInt(r.MaxStderr, DefaultMaxStderr)}

	cmd := exec.CommandContext(tctx, spec.Path, spec.Args...)
	cmd.Dir = r.workDir()
	cmd.Env = []string{"LANG=C.UTF-8", "PATH=" + filepath.Dir(spec.Path)}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the whole process group.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 500 * time.Millisecond
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	cmd.Stdout = out
	cmd.Stderr = errb

	if err := cmd.Start(); err != nil {
		return res, fmt.Errorf("toolrunner: start %s: %w", filepath.Base(spec.Path), err)
	}
	pid := cmd.Process.Pid
	waitErr := cmd.Wait()
	// Reap any stragglers left in the group (best effort).
	_ = syscall.Kill(-pid, syscall.SIGKILL)

	res.Stdout = out.bytes()
	res.Stderr = errb.bytes()
	res.Truncated = out.truncated

	if waitErr != nil {
		if perr := ctx.Err(); perr != nil {
			res.ExitCode = -1
			return res, perr
		}
		if tctx.Err() != nil {
			res.ExitCode = -1
			return res, fmt.Errorf("%w after %s", ErrTimeout, timeout)
		}
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			res.ExitCode = ee.ExitCode() // -1 when signalled
			return res, nil
		}
		return res, fmt.Errorf("toolrunner: wait: %w", waitErr)
	}
	return res, nil
}
