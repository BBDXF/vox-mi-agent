package agent

import (
	"encoding/json"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

func TestRepairJSON(t *testing.T) {
	in := `{"filter": "map(select(.artist == "陈奕迅")) | length", "source": "media_library"}`
	out := repairJSON(in)
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if m["filter"] != `map(select(.artist == "陈奕迅")) | length` || m["source"] != "media_library" {
		t.Fatalf("%v", m)
	}
	ok := `{"a":"x\"y \\\\ z","b":[1,"z"]}`
	if repairJSON(ok) != ok {
		t.Fatalf("valid JSON changed: %s", repairJSON(ok))
	}
}

func TestResolveToolName(t *testing.T) {
	reg := tools.NewRegistry(fakeTool{name: "mcp__fetch__fetch"}, fakeTool{name: "mcp__time__now"}, fakeTool{name: "mcp__clock__now"}, fakeTool{name: "play_audio"})
	r := &Runtime{opt: Options{Tools: reg}}
	for in, want := range map[string]string{
		"play_audio": "play_audio", "fetch__fetch": "mcp__fetch__fetch", "fetch": "mcp__fetch__fetch",
		"now": "now", "nope": "nope", "mcp__fetch__fetch": "mcp__fetch__fetch",
	} {
		if got := r.resolveToolName(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
