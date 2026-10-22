package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/llm"
)

type trackItem struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Artist  string   `json:"artist"`
	Artists []string `json:"artists"`
	Path    string   `json:"path"`
	Ext     string   `json:"ext"`
	Score   *float64 `json:"score"`
}

type tracksResp struct {
	Total int         `json:"total"`
	Items []trackItem `json:"items"`
}

func syncedEnv(t *testing.T, files ...string) *testEnv {
	t.Helper()
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")
	e := newEnv(t, llm.NewFake(), nil)
	music := filepath.Join(e.dir, "music")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(music, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := e.app.Syncer.Trigger(context.Background(), "test"); err != nil || st.Result != "ok" {
		t.Fatalf("sync %+v %v", st, err)
	}
	return e
}

var libFiles = []string{
	"Alan Walker _ Iselin Solheim - Faded.mp3",
	"Alan Walker - Alone.flac",
	"周杰伦、费玉清 - 千里之外.mp3",
	"周杰伦 - 晴天.mp3",
	"周杰伦 - 七里香.mp3",
	"Max x Min - Mix x Match.m4a",
	"无歌手的歌.wav",
}

func titles(r tracksResp) string {
	var out []string
	for _, i := range r.Items {
		out = append(out, i.Title)
	}
	return strings.Join(out, ",")
}

func TestMediaTracksAndArtists(t *testing.T) {
	e := syncedEnv(t, libFiles...)
	get := func(q string) tracksResp {
		t.Helper()
		resp, b := e.do(t, "GET", "/api/media/tracks"+q, "")
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", q, resp.StatusCode, b)
		}
		return decode[tracksResp](t, b)
	}

	// list: sort + paging
	all := get("")
	if all.Total != 7 || len(all.Items) != 7 {
		t.Fatalf("%+v", all)
	}
	if got := titles(get("?sort=title&limit=3")); got != "Alone,Faded,Mix x Match" {
		t.Fatalf("sort=title: %s", got)
	}
	if p := get("?sort=title&limit=3&offset=3"); p.Total != 7 || titles(p) != "七里香,千里之外,无歌手的歌" {
		t.Fatalf("page2: %+v", p)
	}
	if p := get("?offset=100"); p.Total != 7 || len(p.Items) != 0 {
		t.Fatalf("offset past end: %+v", p)
	}
	byArtist := get("?sort=artist")
	if byArtist.Items[0].Artist != "" || byArtist.Items[0].Title != "无歌手的歌" {
		t.Fatalf("sort=artist: %+v", byArtist.Items[0])
	}
	byPath := get("?sort=path")
	for i := 1; i < len(byPath.Items); i++ {
		if byPath.Items[i-1].Path > byPath.Items[i].Path {
			t.Fatalf("sort=path not ordered: %v", byPath.Items)
		}
	}
	first := all.Items[0]
	if first.ID == "" || first.Ext == "" || first.Path == "" || first.Artists == nil || first.Score != nil {
		t.Fatalf("dto: %+v", first)
	}
	for _, i := range all.Items {
		if i.Title == "Faded" && (i.Ext != "mp3" || len(i.Artists) != 2 || i.Artist != "Alan Walker _ Iselin Solheim") {
			t.Fatalf("faded: %+v", i)
		}
	}
	for _, bad := range []string{"?sort=nope", "?limit=0", "?limit=x", "?offset=-1"} {
		if resp, _ := e.do(t, "GET", "/api/media/tracks"+bad, ""); resp.StatusCode != 400 {
			t.Errorf("%s -> %d", bad, resp.StatusCode)
		}
	}

	// artist filter matches any of artists (exact)
	if r := get("?artist=Iselin+Solheim"); r.Total != 1 || r.Items[0].Title != "Faded" {
		t.Fatalf("secondary artist: %+v", r)
	}
	if r := get("?artist=Alan+Walker"); r.Total != 2 || titles(r) != "Alone,Faded" {
		t.Fatalf("Alan Walker: %+v", r)
	}
	if r := get("?artist=Alan"); r.Total != 0 {
		t.Fatalf("not exact: %+v", r)
	}

	// q: fuzzy hits in score order with score
	r := get("?q=" + "晴天")
	if r.Total < 1 || r.Items[0].Title != "晴天" || r.Items[0].Score == nil || *r.Items[0].Score < 65 {
		t.Fatalf("q=晴天: %+v", r)
	}
	for i := 1; i < len(r.Items); i++ {
		if *r.Items[i-1].Score < *r.Items[i].Score {
			t.Fatalf("not score ordered")
		}
	}
	if r := get("?q=Iselin+Solheim"); r.Total < 1 || r.Items[0].Title != "Faded" {
		t.Fatalf("q secondary artist: %+v", r)
	}
	if r := get("?q=" + "周杰伦" + "&artist=" + "费玉清"); r.Total != 1 || r.Items[0].Title != "千里之外" || r.Items[0].Score == nil {
		t.Fatalf("q+artist: %+v", r)
	}
	if r := get("?q=zzzzqqqq"); r.Total != 0 || r.Items == nil {
		t.Fatalf("no hit: %+v", r)
	}
	if r := get("?q=" + "周杰伦" + "&limit=1"); len(r.Items) != 1 || r.Total < 2 {
		t.Fatalf("q paging: %+v", r)
	}

	// artists
	resp, b := e.do(t, "GET", "/api/media/artists", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	ar := decode[struct {
		Items []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"items"`
	}](t, b)
	var got []string
	for _, a := range ar.Items {
		got = append(got, a.Name+":"+strconv.Itoa(a.Count))
	}
	want := "周杰伦:3,Alan Walker:2,Iselin Solheim:1,Max:1,Min:1,费玉清:1"
	if strings.Join(got, ",") != want {
		t.Fatalf("artists\n got %s\nwant %s", strings.Join(got, ","), want)
	}
}

func TestMediaTracksEmptyLibrary(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	for _, p := range []string{"/api/media/tracks", "/api/media/tracks?q=abc", "/api/media/artists"} {
		resp, b := e.do(t, "GET", p, "")
		if resp.StatusCode != 200 || !strings.Contains(string(b), `"items":[]`) {
			t.Errorf("%s: %d %s", p, resp.StatusCode, b)
		}
	}
}

type srcItem struct {
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Format      string   `json:"format"`
	File        string   `json:"file"`
	Exists      bool     `json:"exists"`
	Records     string   `json:"records"`
	Keys        []string `json:"keys"`
	Count       *int     `json:"count"`
	SizeBytes   int64    `json:"size_bytes"`
	UpdatedAt   *string  `json:"updated_at"`
	Description string   `json:"description"`
}

func srcList(t *testing.T, e *testEnv) map[string]srcItem {
	t.Helper()
	resp, b := e.do(t, "GET", "/api/sources", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	r := decode[struct {
		Items []srcItem `json:"items"`
	}](t, b)
	m := map[string]srcItem{}
	for _, i := range r.Items {
		m[i.Name] = i
	}
	return m
}

func TestSourcesBuiltinAndUser(t *testing.T) {
	e := syncedEnv(t, libFiles...)
	// user sources: jsonl + yaml
	srcDir := filepath.Join(e.dir, "sources")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(srcDir, "people.jsonl"), []byte("{\"name\":\"张三\",\"room\":\"a\"}\n{\"name\":\"李四\",\"room\":\"b\"}\n"), 0o644))
	must(os.WriteFile(filepath.Join(srcDir, "devs.yaml"), []byte("devices:\n  - name: 台灯\n    room: a\n  - name: 风扇\n    room: b\n  - name: 空调\n    room: b\n"), 0o644))
	resp, b := e.do(t, "PUT", "/api/settings/sources", `{"items":[
		{"name":"people","file":"people.jsonl","format":"jsonl","keys":["name"]},
		{"name":"devs","file":"devs.yaml","format":"yaml","records":".devices[]","keys":["name"]}]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	// built-in cannot be set through PUT
	resp, b = e.do(t, "PUT", "/api/settings/sources", `{"items":[{"name":"media_library","file":"people.jsonl"}]}`)
	if resp.StatusCode != 400 || !strings.Contains(string(b), "reserved") {
		t.Fatalf("PUT media_library: %d %s", resp.StatusCode, b)
	}

	// list
	l := srcList(t, e)
	if len(l) != 3 {
		t.Fatalf("%+v", l)
	}
	ml := l["media_library"]
	if !ml.Builtin || ml.Format != "jsonl" || !ml.Exists || ml.Count == nil || *ml.Count != 7 ||
		ml.SizeBytes == 0 || ml.UpdatedAt == nil || ml.Description != "本地曲库（同步生成）" ||
		strings.Join(ml.Keys, ",") != "title,artist,alias" || !strings.HasSuffix(ml.File, filepath.Join("media", "songs.jsonl")) {
		t.Fatalf("media_library: %+v", ml)
	}
	if p := l["people"]; p.Builtin || p.Format != "jsonl" || p.Count == nil || *p.Count != 2 || p.UpdatedAt == nil {
		t.Fatalf("people: %+v", p)
	}
	if d := l["devs"]; d.Format != "yaml" || d.Records != ".devices[]" || d.Count == nil || *d.Count != 1 {
		t.Fatalf("devs: %+v", d) // top-level document is not an array -> 1
	}

	// preview
	type prev struct {
		Total int               `json:"total"`
		Items []json.RawMessage `json:"items"`
	}
	resp, b = e.do(t, "GET", "/api/sources/people/preview?limit=1", "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if p := decode[prev](t, b); p.Total != 2 || len(p.Items) != 1 || !strings.Contains(string(p.Items[0]), "张三") {
		t.Fatalf("preview: %s", b)
	}
	resp, b = e.do(t, "GET", "/api/sources/media_library/preview", "")
	if p := decode[prev](t, b); resp.StatusCode != 200 || p.Total != 7 || len(p.Items) != 7 || !strings.Contains(string(p.Items[0]), `"artists"`) {
		t.Fatalf("builtin preview: %s", b)
	}
	resp, b = e.do(t, "GET", "/api/sources/devs/preview", "")
	if p := decode[prev](t, b); resp.StatusCode != 200 || p.Total != 1 || !strings.Contains(string(p.Items[0]), "台灯") {
		t.Fatalf("yaml preview: %s", b)
	}
	if resp, _ = e.do(t, "GET", "/api/sources/nope/preview", ""); resp.StatusCode != 404 {
		t.Fatalf("unknown preview %d", resp.StatusCode)
	}

	// test: exactly what the tool returns
	post := func(name, body string) (int, map[string]any) {
		t.Helper()
		resp, b := e.do(t, "POST", "/api/sources/"+name+"/test", body)
		return resp.StatusCode, decode[map[string]any](t, b)
	}
	code, m := post("media_library", `{"tool":"fuzzy_match","query":"晴天","limit":3}`)
	res, _ := m["results"].([]any)
	if code != 200 || m["query"] != "晴天" || len(res) == 0 || res[0].(map[string]any)["record"].(map[string]any)["title"] != "晴天" {
		t.Fatalf("fuzzy builtin: %d %v", code, m)
	}
	if code, m = post("media_library", `{"tool":"query_json","filter":"select(.title==\"Faded\") | .artists","slurp":false}`); code != 200 ||
		!strings.Contains(m["output"].(string), `["Alan Walker","Iselin Solheim"]`) {
		t.Fatalf("query_json builtin: %d %v", code, m)
	}
	if code, m = post("people", `{"tool":"fuzzy_match","query":"李四"}`); code != 200 || m["count"].(float64) < 1 {
		t.Fatalf("fuzzy people: %d %v", code, m)
	}
	if code, m = post("people", `{"tool":"query_json","filter":".name","raw":true,"slurp":false}`); code != 200 ||
		m["output"] != "张三\n李四\n" {
		t.Fatalf("query_json people: %d %v", code, m)
	}
	if code, m = post("devs", `{"tool":"query_yaml","expr":".devices[] | select(.room == \"b\") | .name","output_format":"json"}`); code != 200 ||
		m["output"] != "\"风扇\"\n\"空调\"\n" {
		t.Fatalf("query_yaml devs: %d %v", code, m)
	}
	// tool-level errors are returned as the tool returns them
	if code, m = post("people", `{"tool":"query_json","filter":"((("}`); code != 200 || m["error"] == nil {
		t.Fatalf("bad filter: %d %v", code, m)
	}
	if code, m = post("people", `{"tool":"fuzzy_match"}`); code != 200 || m["error"] == nil {
		t.Fatalf("missing query: %d %v", code, m)
	}
	if code, _ = post("people", `{"tool":"rm"}`); code != 400 {
		t.Fatalf("bad tool %d", code)
	}
	if code, _ = post("nope", `{"tool":"query_json","filter":"."}`); code != 404 {
		t.Fatalf("unknown source %d", code)
	}
}

func TestBuiltinSourceBeforeSync(t *testing.T) {
	e := newEnv(t, llm.NewFake(), nil)
	l := srcList(t, e)
	ml, ok := l["media_library"]
	if !ok || !ml.Builtin || ml.Exists || ml.Count != nil || ml.UpdatedAt != nil {
		t.Fatalf("%+v", l)
	}
	resp, b := e.do(t, "GET", "/api/sources/media_library/preview", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"total":0`) || !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	resp, b = e.do(t, "POST", "/api/sources/media_library/test", `{"tool":"fuzzy_match","query":"x"}`)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "not been synced") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}
