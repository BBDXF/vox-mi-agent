package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/media"
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

	if got := strings.Join(a.Sources().Names(), ","); got != "media_library" {
		t.Fatalf("names %s", got)
	}
}
