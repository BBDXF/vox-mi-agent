package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/app"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/logx"
)

func TestRunChat(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Tools.Dir = filepath.Join(dir, "no-tools") // missing tools must not abort startup
	cfg.Tools.FzqPath = filepath.Join(cfg.Tools.Dir, "fzq")
	cfg.Tools.JqPath = filepath.Join(cfg.Tools.Dir, "jq")
	cfg.Tools.YqPath = filepath.Join(cfg.Tools.Dir, "yq")
	fake := llm.NewFake(
		llm.FakeTurn{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "query_json",
			Arguments: json.RawMessage(`{"filter":".a","input":"{\"a\":1}"}`)}}},
		llm.FakeTurn{Text: "Hello there."},
		llm.FakeTurn{Text: "Second."},
	)
	a, err := app.Build(cfg, app.WithProvider(fake), app.WithLogger(logx.New("error", io.Discard)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Health.Jq.OK || a.Health.Jq.Error == "" {
		t.Errorf("missing jq should be reported, got %+v", a.Health.Jq)
	}

	var out, errb bytes.Buffer
	in := strings.NewReader("hi\n\n/new\nagain\n/quit\nignored\n")
	if err := runChat(context.Background(), a, "", in, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Hello there.") || !strings.Contains(out.String(), "Second.") {
		t.Errorf("stdout = %q", out.String())
	}
	e := errb.String()
	if !strings.Contains(e, "[tool] query_json") || !strings.Contains(e, "[result] ") || !strings.Contains(e, "[new conversation]") {
		t.Errorf("stderr = %q", e)
	}
	convs, err := a.Store.ListConversations(context.Background(), 10, time.Time{})
	if err != nil || len(convs) != 2 || convs[0].Channel != "cli" {
		t.Errorf("conversations = %+v (%v)", convs, err)
	}
}
