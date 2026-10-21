package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTmp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReservedSourceName(t *testing.T) {
	f := writeTmp(t, "a.json", `[]`)
	if _, err := NewSources([]Source{{Name: "media_library", File: f}}); err == nil {
		t.Fatal("user source named media_library accepted")
	}
	s, err := NewSources([]Source{MediaLibrary("/d/media/songs.jsonl"), {Name: "u", File: f}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range s.List() {
		names = append(names, x.Name)
	}
	if !reflect.DeepEqual(names, []string{"media_library", "u"}) {
		t.Fatal(names)
	}
	if (*Sources)(nil).List() != nil {
		t.Fatal("nil List")
	}
}

func TestCountAndPreview(t *testing.T) {
	jl := writeTmp(t, "s.jsonl", "{\"a\":1}\n\n{\"a\":2}\n{\"a\":3}\n")
	js := writeTmp(t, "s.json", `[{"a":1},{"a":2},{"a":3}]`)
	jobj := writeTmp(t, "o.json", `{"a":1}`)
	ym := writeTmp(t, "s.yaml", "- a: 1\n- a: 2\n- a: 3\n")
	yobj := writeTmp(t, "o.yml", "a: 1\nb: {1: x}\n")
	txt := writeTmp(t, "s.txt", "one\n\ntwo\nthree")
	for _, c := range []struct {
		name  string
		src   Source
		total int
		first string
	}{
		{"jsonl", Source{File: jl, Format: "jsonl"}, 3, `{"a":1}`},
		{"json array", Source{File: js, Format: "json"}, 3, `{"a":1}`},
		{"json object", Source{File: jobj, Format: "json"}, 1, `{"a":1}`},
		{"yaml array", Source{File: ym, Format: "yaml"}, 3, `{"a":1}`},
		{"yaml object", Source{File: yobj, Format: "yaml"}, 1, `{"a":1,"b":{"1":"x"}}`},
		{"auto lines", Source{File: txt}, 3, `"one"`},
		{"auto jsonl", Source{File: jl, Format: "auto"}, 3, `{"a":1}`},
	} {
		n, err := c.src.Count()
		if err != nil || n != c.total {
			t.Errorf("%s: Count = %d, %v", c.name, n, err)
		}
		total, items, err := c.src.Preview(2)
		if err != nil || total != c.total {
			t.Errorf("%s: Preview total = %d, %v", c.name, total, err)
			continue
		}
		if len(items) == 0 || len(items) > 2 {
			t.Errorf("%s: %d items", c.name, len(items))
			continue
		}
		b, err := json.Marshal(items[0])
		if err != nil || string(b) != c.first {
			t.Errorf("%s: first %s %v", c.name, b, err)
		}
	}
	if _, err := (Source{File: filepath.Join(t.TempDir(), "none"), Format: "json"}).Count(); !os.IsNotExist(err) {
		t.Errorf("missing file: %v", err)
	}
}

func TestMissingBuiltinFileIsFriendly(t *testing.T) {
	dir := toolsDir(t)
	srcs, _ := NewSources([]Source{MediaLibrary(filepath.Join(t.TempDir(), "songs.jsonl"))})
	for _, tl := range All(Deps{Sources: srcs, ToolsDir: dir}) {
		var args string
		switch tl.Name() {
		case "fuzzy_match":
			args = `{"query":"x","source":"media_library"}`
		case "query_json":
			args = `{"filter":".","source":"media_library"}`
		default:
			args = `{"expr":".","source":"media_library"}`
		}
		m := call(t, tl, args)
		if msg, _ := m["error"].(string); msg == "" || !strings.Contains(msg, "not been synced") {
			t.Errorf("%s: %v", tl.Name(), m)
		}
	}
}
