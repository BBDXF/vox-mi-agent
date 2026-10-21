package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/media"
)

func body(t *testing.T, js string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func put(t *testing.T, a *App, sec, js string) *PutResult {
	t.Helper()
	res, err := a.PutSection(context.Background(), sec, body(t, js))
	if err != nil {
		t.Fatalf("PUT %s %s: %v", sec, js, err)
	}
	return res
}

func putFields(t *testing.T, a *App, sec, js string) map[string]string {
	t.Helper()
	_, err := a.PutSection(context.Background(), sec, body(t, js))
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("PUT %s %s: want ValidationError, got %v", sec, js, err)
	}
	return ve.Fields
}

func reload(t *testing.T, dir string) *config.Config {
	t.Helper()
	c, err := config.Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return c
}

func TestSchemaListsAllSections(t *testing.T) {
	want := []string{"llm", "media", "aliases", "search", "sources", "tools", "speaker", "logging", "server"}
	sc := Schema()
	if len(sc) != len(want) {
		t.Fatalf("%d sections", len(sc))
	}
	types := map[string]bool{TString: true, TInt: true, TFloat: true, TBool: true, TDuration: true, TByteSize: true,
		TStrings: true, TEnum: true, TObjects: true, TMap: true}
	byName := map[string]SectionSchema{}
	for i, s := range sc {
		if s.Name != want[i] || s.Title == "" || len(s.Fields) == 0 {
			t.Errorf("section %d: %+v", i, s)
		}
		byName[s.Name] = s
		for _, f := range s.Fields {
			if !types[f.Type] || f.Description == "" || f.Key == "" {
				t.Errorf("%s.%s: bad field %+v", s.Name, f.Key, f)
			}
			if f.Type == TEnum && len(f.Enum) == 0 {
				t.Errorf("%s.%s: enum without values", s.Name, f.Key)
			}
			if f.Type == TObjects && len(f.Items) == 0 {
				t.Errorf("%s.%s: object[] without item schema", s.Name, f.Key)
			}
		}
	}
	find := func(sec, key string) Field {
		for _, f := range byName[sec].Fields {
			if f.Key == key {
				return f
			}
		}
		t.Fatalf("missing field %s.%s", sec, key)
		return Field{}
	}
	if !byName["server"].RestartRequired || !find("server", "listen").RestartRequired || byName["llm"].RestartRequired {
		t.Error("restart_required flags wrong")
	}
	if f := find("llm", "api_key"); !f.Secret {
		t.Error("api_key must be secret")
	}
	if f := find("logging", "trace_retention_days"); f.Default != 30 {
		t.Errorf("default %v", f.Default)
	}
	if find("logging", "trace_max_rows").Default != 100000 || find("logging", "debug_payloads").Default != false ||
		find("logging", "payload_retention_days").Default != 7 || find("logging", "payload_max_bytes").Default != "200MiB" {
		t.Error("logging defaults")
	}
	if f := find("tools", "shell.enabled"); !strings.Contains(f.Description, "未实现") {
		t.Error("shell.enabled must say not implemented")
	}
	if f := find("search", "limit"); f.Default != 5 {
		t.Errorf("search.limit default %v", f.Default)
	}
	if find("tools", "fzq_path").Default != "/usr/local/bin/fzq" {
		t.Errorf("fzq default %v", find("tools", "fzq_path").Default)
	}
	b, _ := json.Marshal(sc)
	if !strings.Contains(string(b), `"default":`) || !strings.Contains(string(b), `"restart_required":true`) {
		t.Error("schema JSON shape")
	}
}

func TestConfigAccessorReturnsCopy(t *testing.T) {
	a, _ := newTestApp(t, nil)
	c := a.Config()
	c.Search.StripPatterns[0] = "tampered"
	c.Media.Extensions = append(c.Media.Extensions, "zzz")
	c.LLM.ExtraBody = map[string]any{"x": 1}
	if a.Config().Search.StripPatterns[0] == "tampered" || len(a.Config().Media.Extensions) != 7 || a.Config().LLM.ExtraBody != nil {
		t.Fatal("Config() shares state with the live config")
	}
}

func TestUnknownSectionAndEmptyBody(t *testing.T) {
	a, _ := newTestApp(t, nil)
	if _, err := a.PutSection(context.Background(), "nope", body(t, `{"a":1}`)); !errors.Is(err, ErrUnknownSection) {
		t.Fatalf("%v", err)
	}
	if _, err := a.GetSection(context.Background(), "nope"); !errors.Is(err, ErrUnknownSection) {
		t.Fatalf("%v", err)
	}
	if _, err := a.PutSection(context.Background(), "search", map[string]json.RawMessage{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if f := putFields(t, a, "search", `{"nope":1}`); f["nope"] != "unknown field" {
		t.Fatalf("%v", f)
	}
	if f := putFields(t, a, "search", `{"limit":"x","min_score":null}`); f["limit"] == "" || f["min_score"] == "" {
		t.Fatalf("%v", f)
	}
}

func TestSectionLLM(t *testing.T) {
	a, dir := newTestApp(t, nil)
	res := put(t, a, "llm", `{"model":"m2","temperature":0.5,"timeout":"45s","extra_body":{"enable_thinking":false},"api_key":"sk-sekrit-9999"}`)
	if res.RestartRequired || len(res.Changed) == 0 {
		t.Fatalf("%+v", res)
	}
	c := a.Config().LLM
	if c.Model != "m2" || c.Temperature != 0.5 || c.Timeout != 45*time.Second || c.ExtraBody["enable_thinking"] != false || c.APIKey != "sk-sekrit-9999" {
		t.Fatalf("live %+v", c)
	}
	if a.Provider.Model() != "m2" {
		t.Fatalf("provider model %q", a.Provider.Model())
	}
	r := reload(t, dir)
	if r.LLM.Model != "m2" || r.LLM.Timeout != 45*time.Second || r.LLM.ExtraBody["enable_thinking"] != false || r.LLM.APIKey != "sk-sekrit-9999" {
		t.Fatalf("persisted %+v", r.LLM)
	}
	got, _ := a.GetSection(context.Background(), "llm")
	if got["api_key"] != "****9999" || got["api_key_masked"] != "****9999" || got["has_api_key"] != true || got["timeout"] != "45s" {
		t.Fatalf("GET %+v", got)
	}
	if b, _ := json.Marshal(got); strings.Contains(string(b), "sekrit") {
		t.Fatal("secret leaked in GET")
	}
	// partial update keeps the rest (including base_url and key)
	put(t, a, "llm", `{"temperature":0.9}`)
	if c := a.Config().LLM; c.Model != "m2" || c.Temperature != 0.9 || c.APIKey != "sk-sekrit-9999" || c.Timeout != 45*time.Second {
		t.Fatalf("partial %+v", c)
	}
	// extra_body {} clears
	put(t, a, "llm", `{"extra_body":{}}`)
	if len(a.Config().LLM.ExtraBody) != 0 || len(reload(t, dir).LLM.ExtraBody) != 0 {
		t.Fatal("extra_body not cleared")
	}
	// errors
	if _, err := a.PutSection(context.Background(), "llm", body(t, `{"temperature":9}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if _, err := a.PutSection(context.Background(), "llm", body(t, `{"timeout":"1ms"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	if f := putFields(t, a, "llm", `{"timeout":"abc","bogus":1}`); f["timeout"] == "" || f["bogus"] == "" {
		t.Fatalf("%v", f)
	}
	// audit: key names only, never values
	au, _ := a.Store.ListAudit(context.Background(), "settings", 20)
	if len(au) < 2 {
		t.Fatalf("audit rows %d", len(au))
	}
	all := ""
	for _, e := range au {
		all += string(e.Detail)
	}
	if !strings.Contains(all, `"section":"llm"`) || !strings.Contains(all, "api_key") || strings.Contains(all, "sekrit") || strings.Contains(all, "m2") {
		t.Fatalf("audit detail %s", all)
	}
}

func TestSectionMediaExtraFields(t *testing.T) {
	a, dir := newTestApp(t, nil)
	m1 := musicDir(t, "Artist - Song One.mp3")
	if _, err := a.UpdateMedia(context.Background(), MediaUpdate{Folders: []string{m1}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first sync", func() bool { return a.Library.Len() == 1 && !a.Syncer.Status().Running })
	id := media.TrackID(filepath.Join(m1, "Artist - Song One.mp3"))
	if tr, ok := a.Library.Lookup(id); !ok || tr.Artist != "Artist" || tr.Title != "Song One" {
		t.Fatalf("default pattern track %+v", tr)
	}

	// validation
	for js, key := range map[string]string{
		`{"filename_pattern":"("}`:                     "filename_pattern",
		`{"filename_pattern":"(?P<name>[^/]+)\\.mp3"}`: "filename_pattern",
		`{"max_files":0}`:                              "max_files",
		`{"max_files":1.5}`:                            "max_files",
		`{"sync_on_start":"yes"}`:                      "sync_on_start",
		`{"sync_interval":"30s"}`:                      "sync_interval",
		`{"tag_mode":"bogus"}`:                         "tag_mode",
		`{"tag_mode":5}`:                               "tag_mode",
	} {
		if f := putFields(t, a, "media", js); f[key] == "" {
			t.Errorf("%s: fields %v", js, f)
		}
	}

	// unchanged scalar-only change does not trigger a sync
	before := a.Syncer.Status().LastSyncAt
	res := put(t, a, "media", `{"sync_on_start":true,"sync_interval":"2h"}`)
	if res.SyncStarted == nil || *res.SyncStarted || len(res.Changed) != 2 || a.Scheduler.Interval != 2*time.Hour {
		t.Fatalf("%+v", res)
	}
	time.Sleep(100 * time.Millisecond)
	if !a.Syncer.Status().LastSyncAt.Equal(before) {
		t.Fatal("sync triggered by non-index field")
	}

	// index-affecting change triggers a sync and re-parses the files
	res = put(t, a, "media", `{"max_files":5000,"filename_pattern":"(?P<title>[^/]+)\\.\\w+$"}`)
	if res.SyncStarted == nil {
		t.Fatalf("%+v", res)
	}
	waitFor(t, "re-parse", func() bool {
		tr, ok := a.Library.Lookup(id)
		return ok && tr.Title == "Artist - Song One" && tr.Artist == ""
	})
	r := reload(t, dir)
	if !r.Media.SyncOnStart || r.Media.SyncInterval != 2*time.Hour || r.Media.MaxFiles != 5000 || !strings.Contains(r.Media.FilenamePattern, "(?P<title>") {
		t.Fatalf("persisted %+v", r.Media)
	}
	put(t, a, "media", `{"tag_mode":"tags_first"}`)
	if reload(t, dir).Media.TagMode != "tags_first" {
		t.Fatal("tag_mode not persisted")
	}
	got, _ := a.GetSection(context.Background(), "media")
	if got["tag_mode"] != "tags_first" {
		t.Fatalf("tag_mode %v", got["tag_mode"])
	}
	if got["max_files"] != 5000 || got["sync_on_start"] != true {
		t.Fatalf("%+v", got)
	}
}

func TestSectionAliasesDBAndSync(t *testing.T) {
	a, _ := newTestApp(t, nil)
	m := musicDir(t, "Artist - Song One.mp3")
	if _, err := a.UpdateMedia(context.Background(), MediaUpdate{Folders: []string{m}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first sync", func() bool { return a.Library.Len() == 1 && !a.Syncer.Status().Running })
	id := media.TrackID(filepath.Join(m, "Artist - Song One.mp3"))

	res := put(t, a, "aliases", `{"items":[{"title":"Song One","artist":"Artist","aliases":["uno"," 一号 ","uno"]},{"title":"Other","aliases":[]}]}`)
	if res.SyncStarted == nil || len(res.Changed) != 1 {
		t.Fatalf("%+v", res)
	}
	waitFor(t, "alias applied by sync", func() bool {
		tr, ok := a.Library.Lookup(id)
		return ok && len(tr.Alias) == 2 && tr.Alias[0] == "uno" && tr.Alias[1] == "一号"
	})
	got, _ := a.GetSection(context.Background(), "aliases")
	items := got["items"]
	b, _ := json.Marshal(items)
	if !strings.Contains(string(b), `"title":"Other"`) || !strings.Contains(string(b), `"aliases":["uno","一号"]`) {
		t.Fatalf("GET %s", b)
	}
	// same content again: no change, no sync
	waitFor(t, "idle", func() bool { return !a.Syncer.Status().Running })
	before := a.Syncer.Status().LastSyncAt
	res = put(t, a, "aliases", `{"items":[{"title":"Other","aliases":[]},{"title":"Song One","artist":"Artist","aliases":["uno","一号"]}]}`)
	if len(res.Changed) != 0 || *res.SyncStarted {
		t.Fatalf("%+v", res)
	}
	time.Sleep(100 * time.Millisecond)
	if !a.Syncer.Status().LastSyncAt.Equal(before) {
		t.Fatal("sync on unchanged aliases")
	}
	// validation
	long := strings.Repeat("a", 201)
	var manyL []string
	for i := 0; i < 21; i++ {
		manyL = append(manyL, `"a`+strings.Repeat("x", i)+`"`)
	}
	many := strings.Join(manyL, ",")
	f := putFields(t, a, "aliases", `{"items":[{"title":""},{"title":"d","aliases":["a"]},{"title":"d","aliases":["b"]},{"title":"`+long+`"},{"title":"m","aliases":[`+many+`]}]}`)
	for _, k := range []string{"items[0].title", "items[2].title", "items[3].title"} {
		if f[k] == "" {
			t.Errorf("missing %s in %v", k, f)
		}
	}
	if f["items[4].aliases"] == "" {
		t.Errorf("alias count %v", f)
	}
	if f := putFields(t, a, "aliases", `{"items":[{"title":"x","bogus":1}]}`); f["items"] == "" {
		t.Errorf("%v", f)
	}
	if f := putFields(t, a, "aliases", `{"other":1}`); f["other"] == "" || f["items"] == "" {
		t.Errorf("%v", f)
	}
	// clear
	put(t, a, "aliases", `{"items":[]}`)
	waitFor(t, "alias cleared", func() bool {
		tr, ok := a.Library.Lookup(id)
		return ok && len(tr.Alias) == 0 && !a.Syncer.Status().Running
	})
}

func TestSectionSearchLive(t *testing.T) {
	a, dir := newTestApp(t, nil)
	m := musicDir(t, "song alpha.mp3", "song beta.mp3", "song gamma.mp3")
	if _, err := a.UpdateMedia(context.Background(), MediaUpdate{Folders: []string{m}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sync", func() bool { return a.Library.Len() == 3 && !a.Syncer.Status().Running })
	tool, ok := a.Tools.Get("search_audio")
	if !ok {
		t.Fatal("no search_audio tool")
	}
	count := func() int {
		v, err := tool.Call(context.Background(), json.RawMessage(`{"query":"song"}`))
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)["count"].(int)
	}
	if n := count(); n != 3 {
		t.Fatalf("default limit gave %d hits", n)
	}

	res := put(t, a, "search", `{"strip_patterns":["^foo","bar$"],"min_score":70,"limit":2,"candidates":100}`)
	if res.RestartRequired || len(res.Changed) != 4 {
		t.Fatalf("%+v", res)
	}
	if p := a.Index.Params(); p.Candidates != 100 || len(p.Strip) != 2 || p.Strip[0] != "^foo" {
		t.Fatalf("index params %+v", p)
	}
	if n := count(); n != 2 { // next query already uses limit=2
		t.Fatalf("after limit=2 got %d hits", n)
	}
	r := reload(t, dir)
	if r.Search.Limit != 2 || r.Search.MinScore != 70 || r.Search.Candidates != 100 || len(r.Search.StripPatterns) != 2 {
		t.Fatalf("persisted %+v", r.Search)
	}
	for js, key := range map[string]string{
		`{"strip_patterns":["ok","("]}`: "strip_patterns[1]",
		`{"min_score":101}`:             "min_score",
		`{"min_score":-1}`:              "min_score",
		`{"limit":0}`:                   "limit",
		`{"limit":21}`:                  "limit",
		`{"candidates":49}`:             "candidates",
		`{"candidates":5001}`:           "candidates",
	} {
		if f := putFields(t, a, "search", js); f[key] == "" {
			t.Errorf("%s -> %v", js, f)
		}
	}
	if a.Config().Search.Limit != 2 {
		t.Fatal("invalid update changed the live config")
	}
}

func TestSectionSourcesLive(t *testing.T) {
	a, dir := newTestApp(t, nil)
	srcDir := filepath.Join(dir, "sources")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "people.json"), []byte(`[{"name":"张三"},{"name":"李四"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(t.TempDir(), "abs.json")
	if err := os.WriteFile(abs, []byte(`{"k":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	qj, _ := a.Tools.Get("query_json")
	call := func(src string) map[string]any {
		v, err := qj.Call(context.Background(), json.RawMessage(`{"filter":".[].name","source":"`+src+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)
	}
	if r := call("people"); r["error"] == nil || !strings.Contains(r["error"].(string), "unknown source") {
		t.Fatalf("before: %+v", r)
	}
	res := put(t, a, "sources", `{"items":[{"name":"people","file":"people.json","format":"json","keys":["name"]},{"name":"abs_file","file":"`+abs+`"}]}`)
	if res.RestartRequired || res.Applied["sources"] == nil {
		t.Fatalf("%+v", res)
	}
	if r := call("people"); r["error"] != nil || !strings.Contains(r["output"].(string), "张三") {
		t.Fatalf("after: %+v", r)
	}
	got, _ := a.GetSection(context.Background(), "sources")
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"name":"abs_file"`) || !strings.Contains(string(b), `"format":"auto"`) {
		t.Fatalf("GET %s", b)
	}
	r := reload(t, dir)
	if len(r.Sources) != 2 || r.Sources[0].Name != "people" || r.Sources[0].Keys[0] != "name" || r.Sources[1].File != abs {
		t.Fatalf("persisted %+v", r.Sources)
	}
	// swap: remove people
	put(t, a, "sources", `{"items":[{"name":"abs_file","file":"`+abs+`"}]}`)
	if r := call("people"); r["error"] == nil {
		t.Fatalf("people should be gone: %+v", r)
	}
	// validation
	long := "a" + strings.Repeat("b", 32)
	f := putFields(t, a, "sources", `{"items":[
		{"name":"Bad","file":"people.json"},
		{"name":"dup","file":"people.json"},{"name":"dup","file":"people.json"},
		{"name":"`+long+`","file":"people.json"},
		{"name":"missing","file":"nope.json"},
		{"name":"esc","file":"../x.json"},
		{"name":"fmt","file":"people.json","format":"xml"},
		{"name":"k5","file":"people.json","keys":["a","b","c","d","e"]},
		{"name":"rx","file":"people.json","extract":"(","strip":["ok","("]}]}`)
	for _, k := range []string{"items[0].name", "items[2].name", "items[3].name", "items[4].file", "items[5].file",
		"items[6].format", "items[7].keys", "items[8].extract", "items[8].strip[1]"} {
		if f[k] == "" {
			t.Errorf("missing %s in %v", k, f)
		}
	}
	if len(r.Sources) != 2 || len(a.Config().Sources) != 1 {
		t.Fatalf("live sources %+v", a.Config().Sources)
	}
}

func writeScript(t *testing.T, name, out string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+out+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSectionToolsPathsAndShell(t *testing.T) {
	a, dir := newTestApp(t, nil)
	notExec := filepath.Join(t.TempDir(), "jq")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := putFields(t, a, "tools", `{"jq_path":"/definitely/missing/jq","yq_path":"`+notExec+`","shell":{"timeout":"1ms","max_output":"1B","readable_paths":["rel"]}}`)
	for _, k := range []string{"jq_path", "yq_path", "shell.timeout", "shell.max_output", "shell.readable_paths[0]"} {
		if f[k] == "" {
			t.Errorf("missing %s in %v", k, f)
		}
	}
	if f := putFields(t, a, "tools", `{"shell":{"expose_to":"nobody","bogus":1}}`); f["shell.expose_to"] == "" || f["shell.bogus"] == "" {
		t.Errorf("%v", f)
	}

	// the data tools must use the configured per-tool path, not <tools dir>/jq
	fake := writeScript(t, "fakejq", "CUSTOM-JQ")
	res := put(t, a, "tools", `{"jq_path":"`+fake+`","shell":{"enabled":true,"timeout":"20s","max_output":"128KiB","readable_paths":["/srv/x"]}}`)
	if res.RestartRequired {
		t.Fatalf("shell.enabled must not require restart: %+v", res)
	}
	if h, ok := res.Applied["health"].(ToolStatuses); !ok || h.Jq.Path != fake || !h.Fzq.OK {
		t.Fatalf("self-check not re-run: %+v", res.Applied)
	}
	if st := a.HealthStatus(); st.Jq.Path != fake {
		t.Fatalf("HealthStatus %+v", st)
	}
	qj, _ := a.Tools.Get("query_json")
	v, err := qj.Call(context.Background(), json.RawMessage(`{"filter":".","input":"1"}`))
	if err != nil || !strings.Contains(v.(map[string]any)["output"].(string), "CUSTOM-JQ") {
		t.Fatalf("query_json did not use jq_path: %v %v", v, err)
	}
	c := a.Config().Tools
	if c.JqPath != fake || !c.Shell.Enabled || c.Shell.Timeout != 20*time.Second || c.Shell.MaxOutput != 128<<10 || c.Shell.ReadablePaths[0] != "/srv/x" {
		t.Fatalf("live %+v", c)
	}
	r := reload(t, dir)
	if r.Tools.JqPath != fake || !r.Tools.Shell.Enabled || r.Tools.Shell.Timeout != 20*time.Second || r.Tools.Shell.MaxOutput != 128<<10 {
		t.Fatalf("persisted %+v", r.Tools)
	}
	got, _ := a.GetSection(context.Background(), "tools")
	sh := got["shell"].(map[string]any)
	if got["jq_path"] != fake || sh["enabled"] != true || sh["timeout"] != "20s" || sh["max_output"] != "128KiB" || got["health"] == nil {
		t.Fatalf("GET %+v", got)
	}
	// empty path = default location again
	res = put(t, a, "tools", `{"jq_path":""}`)
	if a.Config().Tools.JqPath == fake {
		t.Fatal("jq_path not reset")
	}
}

func TestSectionSpeakerLoggingServer(t *testing.T) {
	lv := new(slog.LevelVar)
	a, dir := newTestApp(t, nil)
	_ = lv
	// speaker
	put(t, a, "speaker", `{"driver":"log"}`)
	if a.Config().Speaker.Driver != "log" || reload(t, dir).Speaker.Driver != "log" {
		t.Fatal("speaker driver")
	}
	if f := putFields(t, a, "speaker", `{"driver":"bogus"}`); f["driver"] == "" {
		t.Fatalf("%v", f)
	}
	// logging: level is live
	res := put(t, a, "logging", `{"level":"debug","trace_retention_days":10,"trace_max_rows":5000,"debug_payloads":true,"payload_retention_days":3,"payload_max_bytes":"50MiB"}`)
	if res.RestartRequired || a.LevelVar.Level() != slog.LevelDebug || !a.Tracer.PayloadEnabled() {
		t.Fatalf("%+v level=%v", res, a.LevelVar.Level())
	}
	r := reload(t, dir)
	if r.Logging.Level != "debug" || r.Logging.TraceRetentionDays != 10 || r.Logging.TraceMaxRows != 5000 || !r.Logging.DebugPayloads ||
		r.Logging.PayloadRetentionDays != 3 || r.Logging.PayloadMaxBytes != 50<<20 {
		t.Fatalf("persisted %+v", r.Logging)
	}
	put(t, a, "logging", `{"level":"warn","debug_payloads":false}`)
	if a.LevelVar.Level() != slog.LevelWarn || a.Tracer.PayloadEnabled() {
		t.Fatal("level/payload toggle not applied")
	}
	got, _ := a.GetSection(context.Background(), "logging")
	if got["level"] != "warn" || got["payload_max_bytes"] != "50MiB" || got["trace_max_rows"] != 5000 {
		t.Fatalf("GET %+v", got)
	}
	for js, key := range map[string]string{
		`{"level":"loud"}`:               "level",
		`{"trace_retention_days":0}`:     "trace_retention_days",
		`{"trace_max_rows":10}`:          "trace_max_rows",
		`{"payload_retention_days":400}`: "payload_retention_days",
		`{"payload_max_bytes":"10KiB"}`:  "payload_max_bytes",
		`{"payload_max_bytes":"lots"}`:   "payload_max_bytes",
		`{"debug_payloads":"yes"}`:       "debug_payloads",
	} {
		if f := putFields(t, a, "logging", js); f[key] == "" {
			t.Errorf("%s -> %v", js, f)
		}
	}
	// server: persisted, restart required, listener untouched
	res = put(t, a, "server", `{"listen":"tcp://0.0.0.0:9999","base_path":"/app/vox","auth":{"require_admin":true}}`)
	if !res.RestartRequired || len(res.Applied) != 0 || len(res.Changed) != 3 {
		t.Fatalf("%+v", res)
	}
	r = reload(t, dir)
	if r.Server.Listen != "tcp://0.0.0.0:9999" || r.Server.BasePath != "/app/vox" || !r.Server.Auth.RequireAdmin {
		t.Fatalf("persisted %+v", r.Server)
	}
	if a.Cfg.Server.Listen == "tcp://0.0.0.0:9999" {
		t.Fatal("startup snapshot must not change")
	}
	got, _ = a.GetSection(context.Background(), "server")
	if got["listen"] != "tcp://0.0.0.0:9999" || got["auth"].(map[string]any)["require_admin"] != true {
		t.Fatalf("GET %+v", got)
	}
	// unchanged value -> no restart needed
	if res = put(t, a, "server", `{"listen":"tcp://0.0.0.0:9999"}`); res.RestartRequired || len(res.Changed) != 0 {
		t.Fatalf("%+v", res)
	}
	for js, key := range map[string]string{
		`{"listen":"http://x"}`:        "listen",
		`{"listen":"tcp://"}`:          "listen",
		`{"base_path":"no-slash"}`:     "base_path",
		`{"base_path":"/a b"}`:         "base_path",
		`{"auth":{"require_admin":1}}`: "auth.require_admin",
	} {
		if f := putFields(t, a, "server", js); f[key] == "" {
			t.Errorf("%s -> %v", js, f)
		}
	}
}

func TestSettingsPreserveCommentsAndOtherSections(t *testing.T) {
	a, dir := newTestApp(t, nil)
	put(t, a, "search", `{"limit":3}`)
	put(t, a, "speaker", `{"driver":"log"}`)
	raw, _ := os.ReadFile(filepath.Join(dir, config.FileName))
	if !strings.Contains(string(raw), "# vox-mi-agent default configuration") || !strings.Contains(string(raw), "# 0 = manual only") {
		t.Fatalf("comments lost:\n%s", raw)
	}
	if fi, _ := os.Stat(filepath.Join(dir, config.FileName)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	r := reload(t, dir)
	if r.Search.Limit != 3 || r.Search.MinScore != 65 || len(r.Search.StripPatterns) != 3 || r.Media.MaxFiles != 100000 {
		t.Fatalf("other keys damaged: %+v %+v", r.Search, r.Media)
	}
}

func TestConcurrentSettingsAreRaceFree(t *testing.T) {
	a, _ := newTestApp(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, _ = a.PutSection(context.Background(), "search", body(t, `{"limit":`+string(rune('2'+(i+j)%5))+`}`))
				_ = a.Config()
				_, _ = a.GetSection(context.Background(), "search")
				_ = a.HealthStatus()
			}
		}(i)
	}
	wg.Wait()
	if l := a.Config().Search.Limit; l < 2 || l > 6 {
		t.Fatalf("limit %d", l)
	}
}

func TestMediaRejectsNestedFolders(t *testing.T) {
	a, _ := newTestApp(t, nil)
	m := musicDir(t, "A - B.mp3")
	sub := filepath.Join(m, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := a.UpdateMedia(context.Background(), MediaUpdate{Folders: []string{m, sub}})
	if err == nil || !strings.Contains(err.Error(), "nested") {
		t.Fatalf("want nested error, got %v", err)
	}
}
