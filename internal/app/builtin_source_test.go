package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/media"
)

func TestBuiltinMediaLibrarySource(t *testing.T) {
	a, dir := newTestApp(t, nil)
	src, err := a.Sources().Get("media_library")
	if err != nil {
		t.Fatal(err)
	}
	wantFile := filepath.Join(dir, "media", "songs.jsonl")
	if !src.Builtin || src.Format != "jsonl" || src.File != wantFile || src.Description != "本地曲库（同步生成）" ||
		strings.Join(src.Keys, ",") != "title,artist,alias" || src.File != media.SongsPath(dir) {
		t.Fatalf("%+v", src)
	}

	// before the first sync the tools give a friendly error
	for _, c := range []struct{ tool, args string }{
		{"query_json", `{"filter":".","source":"media_library"}`},
		{"fuzzy_match", `{"query":"晴天","source":"media_library"}`},
	} {
		v, err := a.Tools.Call(context.Background(), c.tool, json.RawMessage(c.args))
		if err != nil {
			t.Fatal(err)
		}
		msg, _ := v.(map[string]any)["error"].(string)
		if !strings.Contains(msg, "not been synced") {
			t.Errorf("%s: %+v", c.tool, v)
		}
	}

	// user sources are merged with it and survive PUT; the built-in survives too
	abs := filepath.Join(t.TempDir(), "abs.json")
	if err := os.WriteFile(abs, []byte(`[{"n":1}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	put(t, a, "sources", `{"items":[{"name":"mine","file":"`+abs+`","format":"json"}]}`)
	if got := strings.Join(a.Sources().Names(), ","); got != "media_library,mine" {
		t.Fatalf("names %s", got)
	}
	put(t, a, "sources", `{"items":[]}`)
	if got := strings.Join(a.Sources().Names(), ","); got != "media_library" {
		t.Fatalf("names %s", got)
	}
	if s, _ := a.Sources().Get("media_library"); !s.Builtin {
		t.Fatal("builtin flag lost")
	}
}

func TestBuiltinMediaLibraryProtected(t *testing.T) {
	a, dir := newTestApp(t, nil)
	abs := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(abs, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := putFields(t, a, "sources", `{"items":[{"name":"media_library","file":"`+abs+`"}]}`)
	if !strings.Contains(f["items[0].name"], "reserved") {
		t.Fatalf("fields %v", f)
	}
	if s, _ := a.Sources().Get("media_library"); !s.Builtin || s.File != media.SongsPath(dir) {
		t.Fatalf("built-in was modified: %+v", s)
	}
	if len(a.Config().Sources) != 0 {
		t.Fatalf("config changed: %+v", a.Config().Sources)
	}
	// config.yaml with a user source of that name is rejected at load
	c := config.Defaults()
	c.DataDir = dir
	c.Sources = []config.Source{{Name: "media_library", File: abs}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("validate: %v", err)
	}
	if _, err := buildSources(c); err == nil {
		t.Fatal("buildSources accepted reserved name")
	}
}
