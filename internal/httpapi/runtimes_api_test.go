package httpapi

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
)

func TestRuntimesAPI(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	resp, b := e.do(t, "GET", "/api/runtimes", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var l []map[string]any
	if err := json.Unmarshal(b, &l); err != nil || len(l) != 8 {
		t.Fatalf("%v %s", err, b)
	}
	for _, k := range []string{"name", "builtin", "installed", "version", "installing", "error"} {
		if _, ok := l[7][k]; !ok {
			t.Errorf("missing %q in %s", k, b)
		}
	}
	if l[5]["name"] != "bun" || l[6]["name"] != "node" || l[7]["name"] != "uv" || l[7]["installed"] != false {
		t.Fatalf("%s", b)
	}
	if resp, _ := e.do(t, "POST", "/api/runtimes/nope/install", ""); resp.StatusCode != 404 {
		t.Fatalf("install unknown: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", "/api/runtimes/nope", ""); resp.StatusCode != 404 {
		t.Fatalf("delete unknown: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", "/api/runtimes/uv", ""); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
}

func TestRuntimesInstallAccepted(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	e.app.Runtimes.GithubBase = "http://127.0.0.1:1" // unreachable: job fails fast
	resp, b := e.do(t, "POST", "/api/runtimes/uv/install", "")
	if resp.StatusCode != 202 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	var st map[string]any
	if json.Unmarshal(b, &st) != nil || st["name"] != "uv" {
		t.Fatalf("%s", b)
	}
}

func TestRunShellAvailableAndDisable(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	if _, ok := e.app.Tools.Get("run_shell"); !ok {
		t.Fatal("run_shell not registered")
	}
	if d := config.Defaults().Agent.ToolsDisabled; !slices.Contains(d, "run_shell") {
		t.Fatalf("run_shell must be disabled by default: %v", d)
	}
}
