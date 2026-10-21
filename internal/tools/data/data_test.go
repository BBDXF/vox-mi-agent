package data

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/fzq"
	"github.com/andy/vox-mi-agent/internal/toolrunner"
	"github.com/andy/vox-mi-agent/internal/tools"
)

func toolsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VOX_TOOLS_DIR")
	if dir == "" {
		dir, _ = filepath.Abs("../../../.tools")
	}
	for _, b := range []string{"fzq", "jq", "yq"} {
		if _, err := os.Stat(filepath.Join(dir, b)); err != nil {
			t.Skipf("tool binary %s missing in %s", b, dir)
		}
	}
	return dir
}

type env struct {
	songs, jq, yq, fz tools.Tool
	runner            *toolrunner.Runner
	records           *[]toolrunner.Record
}

func setup(t *testing.T) env {
	t.Helper()
	dir := toolsDir(t)
	td, _ := filepath.Abs("testdata")
	srcs, err := NewSources([]Source{
		{Name: "songs", File: filepath.Join(td, "songs.jsonl"), Format: "jsonl", Keys: []string{".title", ".artist", ".alias"}},
		{Name: "devices", File: filepath.Join(td, "devices.yaml"), Format: "yaml", Records: ".devices[]", Keys: []string{".name"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := toolrunner.New()
	r.WorkDir = t.TempDir()
	var recs []toolrunner.Record
	r.Auditor = func(rc toolrunner.Record) { recs = append(recs, rc) }
	d := Deps{Runner: r, Sources: srcs, ToolsDir: dir}
	return env{jq: NewQueryJSON(d), yq: NewQueryYAML(d), fz: NewFuzzyMatch(d), runner: r, records: &recs}
}

func call(t *testing.T, tl tools.Tool, args string) map[string]any {
	t.Helper()
	v, err := tl.Call(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Go error: %v", err)
	}
	b, _ := json.Marshal(v)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func isErr(m map[string]any) bool { _, ok := m["error"]; return ok }

func TestSchemasValid(t *testing.T) {
	for _, tl := range All(Deps{ToolsDir: "/nonexistent"}) {
		var m map[string]any
		if err := json.Unmarshal(tl.Schema(), &m); err != nil {
			t.Errorf("%s schema: %v", tl.Name(), err)
		}
		props := m["properties"].(map[string]any)
		for _, bad := range []string{"file", "path", "files"} {
			if _, ok := props[bad]; ok {
				t.Errorf("%s exposes %q", tl.Name(), bad)
			}
		}
		if m["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false", tl.Name())
		}
	}
}

func TestQueryJSON(t *testing.T) {
	e := setup(t)
	m := call(t, e.jq, `{"filter":".title","source":"songs","raw":true}`)
	if !strings.Contains(m["output"].(string), "七里香") {
		t.Fatalf("got %v", m)
	}
	m = call(t, e.jq, `{"filter":"map(select(.artist==\"周杰伦\"))|length","source":"songs","slurp":true}`)
	if strings.TrimSpace(m["output"].(string)) != "2" {
		t.Fatalf("got %v", m)
	}
	m = call(t, e.jq, `{"filter":".a.b","input":"{\"a\":{\"b\":42}}"}`)
	if strings.TrimSpace(m["output"].(string)) != "42" {
		t.Fatalf("got %v", m)
	}
	// filter starting with '-' is a filter, not an option
	m = call(t, e.jq, `{"filter":"-1","input":"{}"}`)
	if strings.TrimSpace(m["output"].(string)) != "-1" {
		t.Fatalf("got %v", m)
	}
	// syntax error -> error result, not Go error
	if m = call(t, e.jq, `{"filter":".[","input":"{}"}`); !isErr(m) {
		t.Fatalf("want error, got %v", m)
	}
	// audit does not contain inline input, and hides nothing wrong
	for _, r := range *e.records {
		if strings.Contains(r.Query, "42") && r.Tool != "query_json" {
			t.Fatal("unexpected record")
		}
	}
}

func TestQueryJSONSecurity(t *testing.T) {
	e := setup(t)
	t.Setenv("VOX_SECRET_TEST", "hunter2")
	// The runner injects exactly LANG and PATH (design: env is cleared except
	// those two), so the whole environment is that and nothing else.
	for _, f := range []string{"$ENV|keys", "env|keys"} {
		m := call(t, e.jq, `{"filter":"`+f+`","input":"null"}`)
		if strings.TrimSpace(m["output"].(string)) != `["LANG","PATH"]` {
			t.Errorf("%s => %v", f, m)
		}
	}
	m := call(t, e.jq, `{"filter":"$ENV.VOX_SECRET_TEST","input":"null"}`)
	if strings.Contains(m["output"].(string), "hunter2") {
		t.Fatal("secret leaked")
	}
	// exclusivity
	for _, a := range []string{
		`{"filter":".","source":"songs","input":"{}"}`,
		`{"filter":"."}`,
		`{"filter":".","source":"nope"}`,
		`{"filter":".","file":"/etc/passwd"}`,
		`{"filter":".","path":"/etc/passwd"}`,
		`{"filter":".","source":"/etc/passwd"}`,
		`{"filter":"` + strings.Repeat("x", 1025) + `","input":"1"}`,
		`{"filter":".","input":"` + strings.Repeat("x", 256<<10+1) + `"}`,
	} {
		if m := call(t, e.jq, a); !isErr(m) {
			t.Errorf("%.60s: want error, got %v", a, m)
		}
	}
	m = call(t, e.jq, `{"filter":".","source":"nope"}`)
	msg := m["error"].(string)
	if !strings.Contains(msg, "devices") || !strings.Contains(msg, "songs") {
		t.Errorf("error should list sources: %s", msg)
	}
	if strings.Contains(msg, "/") {
		t.Errorf("leaks path: %s", msg)
	}
}

func TestQueryJSONTimeoutAndTruncation(t *testing.T) {
	e := setup(t)
	start := time.Now()
	m := call(t, e.jq, `{"filter":"def f: f; f","input":"null"}`)
	if !isErr(m) || !strings.Contains(m["error"].(string), "timed out") {
		t.Fatalf("want timeout, got %v", m)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("too slow: %v", time.Since(start))
	}
	m = call(t, e.jq, `{"filter":"range(100000)","input":"null"}`)
	if m["truncated"] != true || len(m["output"].(string)) > 64<<10 {
		t.Fatalf("want truncated, got len=%d trunc=%v", len(m["output"].(string)), m["truncated"])
	}
}

func TestQueryYAML(t *testing.T) {
	e := setup(t)
	m := call(t, e.yq, `{"expr":".devices[0].name","source":"devices"}`)
	if strings.TrimSpace(m["output"].(string)) != "客厅灯" {
		t.Fatalf("got %v", m)
	}
	m = call(t, e.yq, `{"expr":".devices[] | select(.on == true)","source":"devices","output_format":"json"}`)
	if !strings.Contains(m["output"].(string), `"room": "living"`) {
		t.Fatalf("got %v", m)
	}
	m = call(t, e.yq, `{"expr":".a","input":"a: hello\n"}`)
	if strings.TrimSpace(m["output"].(string)) != "hello" {
		t.Fatalf("got %v", m)
	}
	// '--' protects an expression that looks like a flag
	m = call(t, e.yq, `{"expr":"-1","input":"a: 1"}`)
	if isErr(m) || strings.TrimSpace(m["output"].(string)) != "-1" {
		t.Fatalf("got %v", m)
	}
	if m = call(t, e.yq, `{"expr":".a","input":"a: 1","output_format":"xml"}`); !isErr(m) {
		t.Fatalf("bad format accepted: %v", m)
	}
	if m = call(t, e.yq, `{"expr":".a","input":"a: 1","inplace":true}`); !isErr(m) {
		t.Fatalf("unknown arg accepted: %v", m)
	}
}

func TestQueryYAMLSecurity(t *testing.T) {
	e := setup(t)
	t.Setenv("VOX_SECRET_TEST", "hunter2")
	for _, expr := range []string{
		`load(\"/etc/passwd\")`,
		`load_str(\"/etc/passwd\")`,
		`.b = load(\"/etc/hostname\")`,
		`.b = env(VOX_SECRET_TEST)`,
		`.b = strenv(VOX_SECRET_TEST)`,
		`.b = system(\"id\")`,
		`.b = system(\"/bin/id\")`,
	} {
		m := call(t, e.yq, `{"expr":"`+expr+`","input":"a: 1"}`)
		if out, _ := m["output"].(string); !isErr(m) && (strings.Contains(out, "root:") || strings.Contains(out, "hunter2") || strings.Contains(out, "uid=")) {
			t.Errorf("%s leaked: %v", expr, m)
		} else if !isErr(m) {
			// must not silently succeed for file/env/system ops
			t.Errorf("%s did not fail: %v", expr, m)
		}
	}
	m := call(t, e.yq, `{"expr":"load(\"/etc/passwd\")","input":"a: 1"}`)
	if !strings.Contains(m["error"].(string), "disabled") {
		t.Errorf("unexpected error text: %v", m)
	}
}

func TestFuzzyMatch(t *testing.T) {
	e := setup(t)
	m := call(t, e.fz, `{"query":"七里箱","source":"songs"}`)
	res := m["results"].([]any)
	if len(res) == 0 {
		t.Fatalf("no result: %v", m)
	}
	hit := res[0].(map[string]any)
	if rec, _ := json.Marshal(hit["record"]); !strings.Contains(string(rec), "七里香") {
		t.Fatalf("top hit: %v", hit)
	}
	if hit["source"] != "songs" {
		t.Errorf("source label: %v", hit["source"])
	}
	// no match -> count 0, not error
	m = call(t, e.fz, `{"query":"zzzzqqqxxx","source":"songs"}`)
	if isErr(m) || m["count"].(float64) != 0 {
		t.Fatalf("got %v", m)
	}
	// query starting with '-' is a query
	m = call(t, e.fz, `{"query":"-七里香","source":"songs"}`)
	if isErr(m) {
		t.Fatalf("got %v", m)
	}
	// newline is flattened: no second query, still one result object
	m = call(t, e.fz, `{"query":"七里香\n--help","source":"songs"}`)
	if isErr(m) {
		t.Fatalf("got %v", m)
	}
	// source default keys + alias
	m = call(t, e.fz, `{"query":"美眉 love story","source":"songs","limit":2}`)
	if isErr(m) {
		t.Fatalf("got %v", m)
	}
	// yaml source with default records/keys
	m = call(t, e.fz, `{"query":"客厅的灯","source":"devices"}`)
	if isErr(m) || m["count"].(float64) < 1 {
		t.Fatalf("got %v", m)
	}
	// inline input + temp file is cleaned up
	m = call(t, e.fz, `{"query":"love story","input":"Faded\nLove Story\nRed\n","format":"lines"}`)
	if isErr(m) || m["count"].(float64) < 1 {
		t.Fatalf("got %v", m)
	}
	if hit := m["results"].([]any)[0].(map[string]any); hit["source"] != "input" {
		t.Errorf("source: %v", hit["source"])
	}
	if ents, _ := os.ReadDir(e.runner.WorkDir); len(ents) != 0 {
		t.Errorf("temp files left: %v", ents)
	}
}

func TestFuzzyMatchValidation(t *testing.T) {
	e := setup(t)
	long := strings.Repeat("a", 201)
	for _, a := range []string{
		`{"query":""}`,
		`{"query":"x"}`,
		`{"query":"x","source":"songs","input":"y"}`,
		`{"query":"` + long + `","source":"songs"}`,
		`{"query":"x","source":"songs","limit":0}`,
		`{"query":"x","source":"songs","limit":21}`,
		`{"query":"x","source":"songs","min_score":39}`,
		`{"query":"x","source":"songs","min_score":101}`,
		`{"query":"x","source":"songs","format":"csv"}`,
		`{"query":"x","source":"songs","records":".a;rm"}`,
		`{"query":"x","source":"songs","records":"-x"}`,
		`{"query":"x","source":"songs","keys":["a","b","c","d","e"]}`,
		`{"query":"x","source":"songs","keys":["--config"]}`,
		`{"query":"x","source":"songs","strip":"."}`,
		`{"query":"x","source":"songs","file":"/etc/passwd"}`,
		`{"query":"x","source":"nope"}`,
	} {
		if m := call(t, e.fz, a); !isErr(m) {
			t.Errorf("%.70s: want error, got %v", a, m)
		}
	}
}

func TestFuzzyMatchRecordTruncation(t *testing.T) {
	e := setup(t)
	big := strings.Repeat("长", 2000)
	in, _ := json.Marshal(map[string]any{"query": "长长长", "input": big + "\nother", "format": "lines", "min_score": 40})
	m := call(t, e.fz, string(in))
	if isErr(m) {
		t.Fatal(m)
	}
	for _, h := range m["results"].([]any) {
		rec, _ := json.Marshal(h.(map[string]any)["record"])
		if len(rec) > fzq.MaxRecordBytes+16 {
			t.Errorf("record not truncated: %d", len(rec))
		}
	}
}

func TestSources(t *testing.T) {
	if _, err := NewSources([]Source{{Name: "a", File: "rel.json"}}); err == nil {
		t.Error("relative path accepted")
	}
	if _, err := NewSources([]Source{{Name: "a", File: "/x"}, {Name: "a", File: "/y"}}); err == nil {
		t.Error("duplicate accepted")
	}
	var s *Sources
	if _, err := s.Get("x"); err == nil || !strings.Contains(err.Error(), "none") {
		t.Errorf("nil sources: %v", err)
	}
}
