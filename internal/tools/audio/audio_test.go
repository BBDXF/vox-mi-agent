package audio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/media"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
)

type fakeSearcher struct {
	hits []media.Hit
	amb  bool
	err  error
	gotN int
	gotQ string
}

func (f *fakeSearcher) Search(_ context.Context, q string, n, _ int) ([]media.Hit, bool, error) {
	f.gotQ, f.gotN = q, n
	return f.hits, f.amb, f.err
}

type fakeLib map[string]media.Track

func (l fakeLib) Lookup(id string) (media.Track, bool) { t, ok := l[id]; return t, ok }

func call(t *testing.T, tl interface {
	Call(context.Context, json.RawMessage) (any, error)
}, args string) map[string]any {
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

func TestSchemasValid(t *testing.T) {
	for _, tl := range All(Deps{}) {
		var v map[string]any
		if err := json.Unmarshal(tl.Schema(), &v); err != nil || v["type"] != "object" {
			t.Errorf("%s: bad schema %v", tl.Name(), err)
		}
		if tl.Description() == "" {
			t.Errorf("%s: no description", tl.Name())
		}
	}
	names := []string{}
	for _, tl := range All(Deps{}) {
		names = append(names, tl.Name())
	}
	if strings.Join(names, ",") != "search_audio,play_audio,set_volume" {
		t.Fatal(names)
	}
}

func TestSearchAudioFake(t *testing.T) {
	fs := &fakeSearcher{hits: []media.Hit{{ID: "a", Title: "晴天", Artist: "周杰伦", Score: 100}, {ID: "b", Title: "晴天2", Score: 98}}, amb: true}
	tl := NewSearchAudio(Deps{Searcher: fs, Limit: 7})
	m := call(t, tl, `{"query":"播放\n晴天"}`)
	if m["count"] != float64(2) || m["ambiguous"] != true || fs.gotN != 7 || fs.gotQ != "播放 晴天" {
		t.Fatalf("%v %+v", m, fs)
	}
	r0 := m["results"].([]any)[0].(map[string]any)
	if len(r0) != 4 || r0["id"] != "a" || r0["title"] != "晴天" || r0["artist"] != "周杰伦" || r0["score"] != float64(100) {
		t.Fatalf("%v", r0)
	}
	call(t, tl, `{"query":"x","limit":3}`)
	if fs.gotN != 3 {
		t.Fatal(fs.gotN)
	}
	for _, bad := range []string{`{}`, `{"query":""}`, `{"query":"x","limit":0}`, `{"query":"x","limit":21}`,
		`{"query":"x","path":"/etc"}`, `{"query":"` + strings.Repeat("a", 201) + `"}`, `not json`} {
		if m := call(t, tl, bad); m["error"] == nil {
			t.Errorf("%s accepted: %v", bad, m)
		}
	}
	fs.err = media.ErrNoLibrary
	if m := call(t, tl, `{"query":"x"}`); m["error"] == nil {
		t.Fatal("expected error map")
	}
	fs.err = errors.New("boom")
	if m := call(t, tl, `{"query":"x"}`); m["error"] == nil {
		t.Fatal("expected error map")
	}
	fs.err, fs.hits = nil, nil
	m = call(t, tl, `{"query":"x"}`)
	if m["count"] != float64(0) || m["results"] == nil {
		t.Fatalf("%v", m)
	}
}

func TestPlayAudioAndVolume(t *testing.T) {
	lp := &LogPlayer{}
	lib := fakeLib{"a": {ID: "a", Title: "晴天", Artist: "周杰伦", Path: "/secret/path.mp3"}}
	d := Deps{Library: lib, Player: lp, URLFor: func(tr media.Track) (string, error) { return "http://h/audio/" + tr.ID + ".mp3?sig=x", nil }}
	pl := NewPlayAudio(d)
	m := call(t, pl, `{"id":"a"}`)
	if m["status"] != "playing" || m["title"] != "晴天" {
		t.Fatalf("%v", m)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "secret") {
		t.Fatal("path leaked")
	}
	if p := lp.Played(); len(p) != 1 || p[0].URL != "http://h/audio/a.mp3?sig=x" || p[0].TrackID != "a" || m["url"] != p[0].URL {
		t.Fatalf("%v", p)
	}
	if m := call(t, pl, `{"url":"https://x.example/a.mp3","title":"T"}`); m["status"] != "playing" || m["url"] != "https://x.example/a.mp3" {
		t.Fatalf("%v", m)
	}
	if p := lp.Played(); len(p) != 2 || p[1].TrackID != "" || p[1].Title != "T" {
		t.Fatalf("%v", p)
	}
	for _, bad := range []string{`{"id":"zzz"}`, `{}`, `{"id":"a","path":"/x"}`, `{"id":"a","url":"http://x/y"}`,
		`{"url":"file:///etc/passwd"}`, `{"url":"ftp://x/y"}`, `{"url":"notaurl"}`} {
		if m := call(t, pl, bad); m["error"] == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if len(lp.Played()) != 2 {
		t.Fatal("played on error")
	}

	sv := NewSetVolume(d)
	if m := call(t, sv, `{"volume":40}`); m["volume"] != float64(40) {
		t.Fatalf("%v", m)
	}
	call(t, sv, `{"volume":0}`)
	call(t, sv, `{"volume":100}`)
	for _, bad := range []string{`{}`, `{"volume":-1}`, `{"volume":101}`, `{"volume":"loud"}`, `{"volume":50.5}`} {
		if m := call(t, sv, bad); m["error"] == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if v := lp.Volumes(); len(v) != 3 || v[0] != 40 || v[1] != 0 || v[2] != 100 {
		t.Fatalf("%v", v)
	}
	if m := call(t, NewSetVolume(Deps{}), `{"volume":5}`); m["error"] == nil {
		t.Fatal("nil player must error")
	}
}

// End to end: real sync + fzq + tools.
func TestEndToEnd(t *testing.T) {
	tools := os.Getenv("VOX_TOOLS_DIR")
	if tools == "" {
		tools, _ = filepath.Abs("../../../.tools")
	}
	if _, err := os.Stat(filepath.Join(tools, "fzq")); err != nil {
		t.Skip("fzq missing")
	}
	root := t.TempDir()
	music := filepath.Join(root, "m")
	os.MkdirAll(music, 0o755)
	for _, f := range []string{"周杰伦 - 晴天.mp3", "陈奕迅 - 孤勇者.flac"} {
		os.WriteFile(filepath.Join(music, f), nil, 0o644)
	}
	cfg := config.Defaults()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.Tools.Dir = tools
	cfg.Media.Folders = []string{music}
	r := toolrunner.New()
	r.WorkDir = filepath.Join(root, "run")
	s := media.NewSyncer(cfg, media.Deps{Runner: r})
	if _, err := s.Trigger(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	lp := &LogPlayer{}
	ts := All(Deps{Searcher: media.NewIndex(cfg, r), Library: s.Library(), Player: lp, URLFor: func(tr media.Track) (string, error) { return "http://h/audio/" + tr.ID + ".mp3", nil }, Limit: cfg.Search.Limit, MinScore: int(cfg.Search.MinScore)})
	m := call(t, ts[0], `{"query":"播放周杰伦的晴天"}`)
	res, _ := m["results"].([]any)
	if len(res) == 0 {
		t.Fatalf("%v", m)
	}
	id := res[0].(map[string]any)["id"].(string)
	if b, _ := json.Marshal(m); strings.Contains(string(b), music) {
		t.Fatal("path leaked")
	}
	if m := call(t, ts[1], `{"id":"`+id+`"}`); m["status"] != "playing" {
		t.Fatalf("%v", m)
	}
	if p := lp.Played(); len(p) != 1 || p[0].Title != "晴天" {
		t.Fatalf("%v", p)
	}
}

type deferPlayer struct{ LogPlayer }

func (*deferPlayer) Play(context.Context, PlayRequest) error { return ErrDeferred }

func TestPlayAudioDeferredReportsQueued(t *testing.T) {
	tl := NewPlayAudio(Deps{Player: &deferPlayer{}})
	res, err := tl.Call(context.Background(), json.RawMessage(`{"url":"http://h/a.mp3"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["status"] != "queued" || m["note"] == nil || m["error"] != nil {
		t.Fatalf("%v", m)
	}
}
