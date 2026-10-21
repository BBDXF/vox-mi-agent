package media

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/config"
)

var sampleFiles = []string{
	"周杰伦 - 晴天.mp3",
	"周杰伦 - 七里香.FLAC",
	"陈奕迅 - 孤勇者.flac",
	"Luis Fonsi - Despacito.mp3",
	"sub/朴树 - 平凡之路.m4a",
	"无歌手的歌.wav",
	"notes.txt",
}

func TestIndexArgs(t *testing.T) {
	got := IndexArgs("/d/songs.jsonl")
	want := []string{"-f", "jsonl", "-k", "title", "-k", "artist", "-k", "artists[]", "-k", "alias", "/d/songs.jsonl"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestParseName(t *testing.T) {
	re := regexp.MustCompile(config.Defaults().Media.FilenamePattern)
	for _, c := range []struct{ rel, artist, title string }{
		{"周杰伦 - 晴天.mp3", "周杰伦", "晴天"},
		{"sub/dir/朴树 - 平凡之路.m4a", "朴树", "平凡之路"},
		{"无歌手的歌.wav", "", "无歌手的歌"},
		{"a - b - c.mp3", "a", "b - c"},
	} {
		a, ti := parseName(re, c.rel)
		if a != c.artist || ti != c.title {
			t.Errorf("%s: got (%q,%q)", c.rel, a, ti)
		}
	}
	// no pattern / non-matching: title = basename without extension
	if a, ti := parseName(nil, "x/y.z.mp3"); a != "" || ti != "y.z" {
		t.Errorf("fallback: %q %q", a, ti)
	}
}

func TestSyncBasicAndAtomicFile(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	// a stale tmp file from a crashed run must not matter
	songs := SongsPath(e.cfg.DataDir)
	if err := os.MkdirAll(filepath.Dir(songs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(songs+".tmp", []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := e.trigger(t)
	if st.Result != ResultOK || st.Tracks != 6 {
		t.Fatalf("status %+v", st)
	}
	if _, err := os.Stat(songs + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tmp file left behind: %v", err)
	}
	raw, _ := os.ReadFile(songs)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 6 {
		t.Fatalf("lines=%d\n%s", len(lines), raw)
	}
	var paths []string
	for _, l := range lines {
		var tr Track
		if err := json.Unmarshal([]byte(l), &tr); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, tr.Path)
		if tr.Alias == nil || tr.ID == "" || !filepath.IsAbs(tr.Path) {
			t.Fatalf("bad track %+v", tr)
		}
		if !strings.Contains(l, `"alias":[]`) {
			t.Fatalf("alias must serialise as []: %s", l)
		}
	}
	for i := 1; i < len(paths); i++ {
		if paths[i-1] >= paths[i] {
			t.Fatalf("not sorted by path: %v", paths)
		}
	}
	if e.lib.Len() != 6 {
		t.Fatalf("library has %d", e.lib.Len())
	}
	// status persisted in settings
	v, err := e.store.GetSetting(context.Background(), SettingStatus)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Status
	if err := json.Unmarshal([]byte(v), &persisted); err != nil || persisted.Tracks != 6 || persisted.Result != ResultOK {
		t.Fatalf("persisted %+v %v", persisted, err)
	}
	if got := e.sync.Status(); got.Tracks != 6 || got.Running {
		t.Fatalf("Status() %+v", got)
	}
	au, _ := e.store.ListAudit(context.Background(), "media.sync", 10)
	if len(au) != 1 {
		t.Fatalf("audit rows: %d", len(au))
	}
}

func TestUnchangedSnapshot(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	e.trigger(t)
	songs := SongsPath(e.cfg.DataDir)
	cache := CacheDir(e.cfg.DataDir)
	fi1, _ := os.Stat(songs)
	c1 := dirNames(t, cache)

	time.Sleep(20 * time.Millisecond)
	st := e.trigger(t)
	if st.Result != ResultUnchanged || st.Tracks != 6 {
		t.Fatalf("want unchanged, got %+v", st)
	}
	fi2, _ := os.Stat(songs)
	if !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Fatal("songs.jsonl rewritten on unchanged sync")
	}
	if !reflect.DeepEqual(c1, dirNames(t, cache)) {
		t.Fatal("cache changed on unchanged sync")
	}

	// a new file -> ok
	touch(t, filepath.Join(e.music, "新歌手 - 新歌.mp3"))
	if st := e.trigger(t); st.Result != ResultOK || st.Tracks != 7 {
		t.Fatalf("after add: %+v", st)
	}
	// alias change -> ok
	if st := e.trigger(t); st.Result != ResultUnchanged {
		t.Fatalf("want unchanged, got %+v", st)
	}
	e.aliases.Set("Despacito", []string{"迪斯帕西托"})
	if st := e.trigger(t); st.Result != ResultOK {
		t.Fatalf("alias change must resync: %+v", st)
	}
	// extension filter is case-insensitive: .FLAC counted above (6 tracks)
	// deleted songs file -> resync even if snapshot matches
	if err := os.Remove(songs); err != nil {
		t.Fatal(err)
	}
	if st := e.trigger(t); st.Result != ResultOK {
		t.Fatalf("missing songs.jsonl must resync: %+v", st)
	}
}

func TestUnchangedWithoutStore(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	s := NewSyncer(e.cfg, Deps{Runner: e.runner})
	if st, err := s.Trigger(context.Background(), "t"); err != nil || st.Result != ResultOK {
		t.Fatalf("%+v %v", st, err)
	}
	if st, err := s.Trigger(context.Background(), "t"); err != nil || st.Result != ResultUnchanged {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestCachePrebuiltReusedAndPruned(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	cache := CacheDir(e.cfg.DataDir)
	// pre-seed stale caches
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, n := range []string{"old1.gob", "old2.gob", "old3.gob"} {
		p := filepath.Join(cache, n)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		old = old.Add(time.Hour)
		os.Chtimes(p, old, old)
	}
	e.trigger(t)
	names := dirNames(t, cache)
	if len(names) != KeepCaches {
		t.Fatalf("want %d cache files, got %v", KeepCaches, names)
	}
	var fresh string
	for _, n := range names {
		if !strings.HasPrefix(n, "old") {
			fresh = n
		}
	}
	if fresh == "" || !contains(names, "old3.gob") {
		t.Fatalf("expected new cache + newest old one, got %v", names)
	}
	fi, _ := os.Stat(filepath.Join(cache, fresh))

	// queries reuse the prebuilt cache: no new file, cache untouched
	for _, q := range []string{"晴天", "播放周杰伦的七里香", "平凡之路"} {
		hits, _, err := e.index.Search(context.Background(), q, 3, 65)
		if err != nil || len(hits) == 0 {
			t.Fatalf("%q: %v %v", q, hits, err)
		}
	}
	if got := dirNames(t, cache); !reflect.DeepEqual(got, names) {
		t.Fatalf("query changed cache dir: %v -> %v", names, got)
	}
	fi2, _ := os.Stat(filepath.Join(cache, fresh))
	if !fi2.ModTime().Equal(fi.ModTime()) {
		t.Fatal("cache file was rewritten by a query")
	}

	// new content -> new cache; still <= 2 files
	touch(t, filepath.Join(e.music, "X - 新增.mp3"))
	e.trigger(t)
	after := dirNames(t, cache)
	if len(after) != KeepCaches || !contains(after, fresh) {
		t.Fatalf("after resync: %v (prev fresh %s)", after, fresh)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestIDStability(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	e.trigger(t)
	tr1, _ := e.lib.Lookup(TrackID(filepath.Join(e.music, "sub/朴树 - 平凡之路.m4a")))
	if tr1.Title != "平凡之路" || tr1.Artist != "朴树" {
		t.Fatalf("%+v", tr1)
	}
	sum := sha1.Sum([]byte(filepath.ToSlash(filepath.Join(e.music, "sub/朴树 - 平凡之路.m4a"))))
	if tr1.ID != hex.EncodeToString(sum[:])[:12] {
		t.Fatalf("id %s", tr1.ID)
	}
	snapshot := map[string]string{}
	for id, tr := range e.lib.byID {
		snapshot[id] = tr.Path
	}
	raw1, _ := os.ReadFile(SongsPath(e.cfg.DataDir))

	// full rebuild with a brand-new syncer (no snapshot) -> byte-identical file, same ids
	s2 := NewSyncer(e.cfg, Deps{Runner: e.runner})
	if st, err := s2.Trigger(context.Background(), "t"); err != nil || st.Result != ResultOK {
		t.Fatalf("%+v %v", st, err)
	}
	raw2, _ := os.ReadFile(SongsPath(e.cfg.DataDir))
	if string(raw1) != string(raw2) {
		t.Fatal("same content must give identical bytes")
	}
	for id, p := range snapshot {
		if tr, ok := s2.Library().Lookup(id); !ok || tr.Path != p {
			t.Fatalf("id %s unstable", id)
		}
	}
	// adding a file does not change other ids
	touch(t, filepath.Join(e.music, "A - B.mp3"))
	s2.Trigger(context.Background(), "t")
	for id := range snapshot {
		if _, ok := s2.Library().Lookup(id); !ok {
			t.Fatalf("id %s lost after adding a file", id)
		}
	}
	// same relative path in another folder gets another id; cleaning is applied
	if TrackID("/m1/a.mp3") == TrackID("/m2/a.mp3") || TrackID("/m1/x/../a.mp3") != TrackID("/m1/a.mp3") {
		t.Fatal("id must be derived from the cleaned absolute path")
	}
}

func TestNoSymlinkFollow(t *testing.T) {
	outside := t.TempDir()
	touch(t, filepath.Join(outside, "外部 - 歌曲.mp3"))
	e := newEnv(t, []string{"真 - 歌.mp3"}, nil)
	if err := os.Symlink(outside, filepath.Join(e.music, "linkdir")); err != nil {
		t.Skip("symlink unsupported")
	}
	if err := os.Symlink(filepath.Join(outside, "外部 - 歌曲.mp3"), filepath.Join(e.music, "linkfile.mp3")); err != nil {
		t.Fatal(err)
	}
	st := e.trigger(t)
	if st.Tracks != 1 {
		t.Fatalf("symlinks followed: %+v", st)
	}
	for _, tr := range e.lib.byID {
		rel, _ := filepath.Rel(e.music, tr.Path)
		if rel != "真 - 歌.mp3" {
			t.Fatalf("indexed %s", tr.Path)
		}
	}
	// a configured root that is itself a symlink works (mount points)
	root := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(e.music, root); err != nil {
		t.Fatal(err)
	}
	e.cfg.Media.Folders = []string{root}
	s := NewSyncer(e.cfg, Deps{Runner: e.runner})
	if st, err := s.Trigger(context.Background(), "t"); err != nil || st.Tracks != 1 {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestMaxFilesCap(t *testing.T) {
	var files []string
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		files = append(files, "x - "+n+".mp3")
	}
	e := newEnv(t, files, func(c *config.Config) { c.Media.MaxFiles = 3 })
	st := e.trigger(t)
	if st.Tracks != 3 || st.Warning == "" {
		t.Fatalf("%+v", st)
	}
	if e.lib.Len() != 3 {
		t.Fatalf("lib %d", e.lib.Len())
	}
}

func TestFolderValidation(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	run := func(folders ...string) error {
		e.cfg.Media.Folders = folders
		s := NewSyncer(e.cfg, Deps{Runner: e.runner})
		_, err := s.Trigger(context.Background(), "t")
		return err
	}
	if err := run("relative/dir"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative: %v", err)
	}
	if err := run(filepath.Join(e.music, "nope")); err == nil {
		t.Fatal("missing folder accepted")
	}
	if err := run(); err != nil { // no folders = empty library, not an error
		t.Fatalf("no folders: %v", err)
	}
	// a failed sync must not wipe an existing library
	if err := run(e.music); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(SongsPath(e.cfg.DataDir))
	_ = run(filepath.Join(e.music, "nope"))
	after, err := os.ReadFile(SongsPath(e.cfg.DataDir))
	if err != nil || len(before) == 0 || string(before) != string(after) {
		t.Fatal("songs file lost/changed after failed sync")
	}

	allowed := t.TempDir()
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "/nonexistent/a:"+allowed)
	if err := run(e.music); err == nil || !strings.Contains(err.Error(), "TRIM_DATA_ACCESSIBLE_PATHS") {
		t.Fatalf("outside: %v", err)
	}
	// prefix-but-not-child must be rejected
	if err := run(allowed + "-evil"); err == nil {
		t.Fatal("sibling prefix accepted")
	}
	inside := filepath.Join(allowed, "m")
	touch(t, filepath.Join(inside, "a - b.mp3"))
	if err := run(inside); err != nil {
		t.Fatalf("inside: %v", err)
	}
	if err := run(allowed); err != nil {
		t.Fatalf("root of allowed: %v", err)
	}
}

type blockingAliases struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingAliases) Aliases(context.Context) (map[string][]string, error) {
	if b.calls.Add(1) == 1 {
		close(b.entered)
		<-b.release
	}
	return nil, nil
}

func TestSingleFlight(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	ba := &blockingAliases{entered: make(chan struct{}), release: make(chan struct{})}
	s := NewSyncer(e.cfg, Deps{Runner: e.runner, Store: e.store, Aliases: ba})

	if err := s.TriggerAsync("a"); err != nil {
		t.Fatal(err)
	}
	<-ba.entered
	if !s.Status().Running {
		t.Fatal("Status().Running false while running")
	}
	if err := s.TriggerAsync("b"); !errors.Is(err, ErrRunning) {
		t.Fatalf("want ErrRunning, got %v", err)
	}
	var wg sync.WaitGroup
	res := make([]Status, 3)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := s.Trigger(context.Background(), "join")
			if err != nil {
				t.Error(err)
			}
			res[i] = st
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(ba.release)
	wg.Wait()
	for _, r := range res {
		if r.Result != ResultOK || r.Tracks != 6 {
			t.Fatalf("joiner got %+v", r)
		}
	}
	if n := ba.calls.Load(); n != 1 {
		t.Fatalf("sync body ran %d times", n)
	}
	au, _ := e.store.ListAudit(context.Background(), "media.sync", 10)
	if len(au) != 1 {
		t.Fatalf("audit rows %d", len(au))
	}
	if s.Status().Running {
		t.Fatal("still running")
	}
}

func TestAliasSearchAndNoPathExposure(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	e.aliases.Set("Despacito", []string{"迪斯帕西托"})
	e.trigger(t)
	hits, _, err := e.index.Search(context.Background(), "我想听迪斯帕西托", 3, 65)
	if err != nil || len(hits) == 0 || hits[0].Title != "Despacito" || hits[0].Artist != "Luis Fonsi" {
		t.Fatalf("%+v %v", hits, err)
	}
	b, _ := json.Marshal(hits)
	if strings.Contains(string(b), "path") || strings.Contains(string(b), e.music) {
		t.Fatalf("path leaked: %s", b)
	}
	// no hit / control command are not errors
	if h, amb, err := e.index.Search(context.Background(), "下一首", 3, 65); err != nil || len(h) != 0 || amb {
		t.Fatalf("%v %v %v", h, amb, err)
	}
	if h, _, err := e.index.Search(context.Background(), "  ", 3, 65); err != nil || h != nil {
		t.Fatalf("%v %v", h, err)
	}
}

func TestSearchBeforeSync(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	if _, _, err := e.index.Search(context.Background(), "晴天", 3, 65); !errors.Is(err, ErrNoLibrary) {
		t.Fatalf("got %v", err)
	}
}

type fakeTrigger struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeTrigger) Trigger(_ context.Context, src string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, src)
	return Status{}, nil
}
func (f *fakeTrigger) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestScheduler(t *testing.T) {
	// manual only, no start sync
	ft := &fakeTrigger{}
	ctx, cancel := context.WithCancel(context.Background())
	sc := &Scheduler{T: ft}
	sc.Start(ctx)
	sc.Wait()
	cancel()
	if len(ft.snapshot()) != 0 {
		t.Fatalf("manual scheduler ran: %v", ft.snapshot())
	}

	// on start + periodic
	ft = &fakeTrigger{}
	ctx, cancel = context.WithCancel(context.Background())
	sc = &Scheduler{T: ft, Interval: 20 * time.Millisecond, OnStart: true}
	sc.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for len(ft.snapshot()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	sc.Wait()
	c := ft.snapshot()
	if len(c) < 3 || c[0] != "start" || c[1] != "timer" {
		t.Fatalf("calls %v", c)
	}
}

func TestSchedulerFromConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Media.SyncInterval = 0
	cfg.Media.SyncOnStart = true
	sc := NewScheduler(NewSyncer(cfg, Deps{}))
	if !sc.OnStart || sc.Interval != 0 {
		t.Fatalf("%+v", sc)
	}
	cfg.Media.Folders = nil
	if NewScheduler(NewSyncer(cfg, Deps{})).OnStart {
		t.Fatal("no folders => no start sync")
	}
}

func TestSetMediaAndEmptyLibrary(t *testing.T) {
	e := newEnv(t, sampleFiles, nil)
	e.trigger(t)
	if e.lib.Len() == 0 {
		t.Fatal("empty library")
	}
	m := e.sync.MediaConfig()
	m.Folders = nil
	e.sync.SetMedia(m)
	if st, err := e.sync.Trigger(context.Background(), "t"); err != nil || st.Tracks != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	if e.lib.Len() != 0 {
		t.Fatalf("library not cleared: %d", e.lib.Len())
	}
	if hits, _, err := e.index.Search(context.Background(), "anything", 3, 0); err != nil || len(hits) != 0 {
		t.Fatalf("search on empty library: %v %v", hits, err)
	}
}

func TestSchedulerSetInterval(t *testing.T) {
	ft := &fakeTrigger{}
	ctx, cancel := context.WithCancel(context.Background())
	sc := &Scheduler{T: ft}
	sc.Start(ctx)
	sc.SetInterval(20 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for len(ft.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(ft.snapshot()) < 2 {
		t.Fatalf("no timer syncs after SetInterval: %v", ft.snapshot())
	}
	sc.SetInterval(0) // back to manual
	time.Sleep(60 * time.Millisecond)
	n := len(ft.snapshot())
	time.Sleep(80 * time.Millisecond)
	if len(ft.snapshot()) != n {
		t.Fatal("scheduler still ticking after SetInterval(0)")
	}
	cancel()
	sc.Wait()
}

func TestIndexReconfigureChangesNextQueryArgs(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Tools.Dir = "/x"
	ix := NewIndex(cfg, nil)
	join := func(topN, minScore int) string {
		return strings.Join(ix.searchArgs(ix.Params(), topN, minScore), " ")
	}
	before := join(5, 65)
	if !strings.Contains(before, "--candidates 500") || !strings.Contains(before, "--min-score 65") || !strings.Contains(before, "--strip ^(下一首") {
		t.Fatalf("defaults: %s", before)
	}
	ix.Reconfigure("/other/fzq", []string{"^only$"}, 123)
	after := join(2, 70)
	if !strings.Contains(after, "--candidates 123") || !strings.Contains(after, "--min-score 70") || !strings.Contains(after, "-n 2") ||
		strings.Count(after, "--strip") != 1 || !strings.Contains(after, "--strip ^only$") {
		t.Fatalf("after: %s", after)
	}
	if ix.Params().FzqPath != "/other/fzq" {
		t.Fatalf("%+v", ix.Params())
	}
	// the songs file stays the last argument (cache key invariant)
	args := ix.searchArgs(ix.Params(), 1, 65)
	if args[len(args)-1] != SongsPath(cfg.DataDir) {
		t.Fatalf("songs file not last: %v", args)
	}
	ix.Reconfigure("", nil, 0) // empty fzq keeps the path; 0 candidates => default 500
	if ix.Params().FzqPath != "/other/fzq" || !strings.Contains(join(1, 65), "--candidates 500") {
		t.Fatal("reconfigure defaults")
	}
}

func TestUniqueTrackIDCollision(t *testing.T) {
	a, b := "/m/a.mp3", "/m/b.mp3"
	taken := map[string]string{}
	ida := uniqueTrackID(a, taken)
	taken[ida] = a
	// force a fake collision: pretend b's short id is already held by a
	taken[TrackID(b)] = a
	idb := uniqueTrackID(b, taken)
	if len(ida) != idLen || len(idb) != 16 || idb[:idLen] != TrackID(b) {
		t.Fatalf("ida=%s idb=%s", ida, idb)
	}
	// same path keeps its id
	if uniqueTrackID(a, taken) != ida {
		t.Fatal("same path must keep id")
	}
	// NFC and NFD spellings of the same name give the same id
	if TrackID("/m/caf\u00e9.mp3") != TrackID("/m/cafe\u0301.mp3") {
		t.Fatal("unicode normalisation")
	}
}
