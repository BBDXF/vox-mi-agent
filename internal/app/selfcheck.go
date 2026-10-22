package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
)

// ToolStatus is the startup self-check result of one external binary.
type ToolStatus struct {
	Path    string `json:"path"`
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// ToolStatuses holds the three external tools.
type ToolStatuses struct {
	Fzq ToolStatus `json:"fzq"`
	Jq  ToolStatus `json:"jq"`
	Yq  ToolStatus `json:"yq"`
}

const selfCheckTimeout = 5 * time.Second

// SelfCheck probes fzq / jq / yq. It never returns an error: failures are
// recorded per tool.
func SelfCheck(ctx context.Context, fzqPath, jqPath, yqPath string) ToolStatuses {
	r := toolrunner.New()
	var out ToolStatuses
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); out.Fzq = checkFzq(ctx, r, fzqPath) }()
	go func() { defer wg.Done(); out.Jq = checkJq(ctx, r, jqPath) }()
	go func() { defer wg.Done(); out.Yq = checkYq(ctx, r, yqPath) }()
	wg.Wait()
	return out
}

func probe(ctx context.Context, r *toolrunner.Runner, tool, path string, args ...string) (toolrunner.Result, error) {
	return r.Run(ctx, toolrunner.Spec{Tool: tool, Path: path, Args: args, Timeout: selfCheckTimeout})
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func failed(st ToolStatus, err error) ToolStatus {
	st.OK = false
	st.Error = err.Error()
	return st
}

func checkJq(ctx context.Context, r *toolrunner.Runner, path string) ToolStatus {
	st := ToolStatus{Path: path}
	res, err := probe(ctx, r, "jq", path, "--version")
	if err != nil {
		return failed(st, err)
	}
	if res.ExitCode != 0 {
		return failed(st, fmt.Errorf("jq --version exited %d: %s", res.ExitCode, firstLine(res.Stderr)))
	}
	st.Version = strings.TrimPrefix(firstLine(res.Stdout), "jq-")
	st.OK = true
	return st
}

func checkYq(ctx context.Context, r *toolrunner.Runner, path string) ToolStatus {
	st := ToolStatus{Path: path}
	res, err := probe(ctx, r, "yq", path, "--version")
	if err != nil {
		return failed(st, err)
	}
	if res.ExitCode != 0 {
		return failed(st, fmt.Errorf("yq --version exited %d: %s", res.ExitCode, firstLine(res.Stderr)))
	}
	line := firstLine(res.Stdout)
	if i := strings.LastIndex(line, "version "); i >= 0 {
		st.Version = strings.TrimSpace(line[i+len("version "):])
	} else {
		st.Version = line
	}
	// The safety switches are mandatory (§6.9): refuse yq builds lacking them.
	res, err = probe(ctx, r, "yq", path, "--security-disable-file-ops", "--security-disable-env-ops", "-n", "1")
	if err != nil {
		return failed(st, err)
	}
	if res.ExitCode != 0 || !bytes.Equal(bytes.TrimSpace(res.Stdout), []byte("1")) {
		return failed(st, errors.New("yq does not support --security-disable-file-ops/--security-disable-env-ops: "+firstLine(res.Stderr)))
	}
	st.OK = true
	return st
}

func checkFzq(ctx context.Context, r *toolrunner.Runner, path string) ToolStatus {
	st := ToolStatus{Path: path}
	// fzq has no --version flag: the first help line is "fzq vX.Y.Z".
	res, err := probe(ctx, r, "fzq", path, "-h")
	if err != nil {
		return failed(st, err)
	}
	out := res.Stdout
	if len(bytes.TrimSpace(out)) == 0 {
		out = res.Stderr
	}
	line := firstLine(out)
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "fzq" {
		return failed(st, fmt.Errorf("unexpected fzq help output %q", line))
	}
	st.Version = f[1]
	st.OK = true
	return st
}
