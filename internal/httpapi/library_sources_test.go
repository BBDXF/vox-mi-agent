package httpapi

import (
	"context"
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
