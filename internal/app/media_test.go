package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
)

func newTestApp(t *testing.T, mod func(*config.Config)) (*App, string) {
	t.Helper()
	tools := os.Getenv("VOX_TOOLS_DIR")
	if tools == "" {
		tools, _ = filepath.Abs("../../.tools")
	}
	if _, err := os.Stat(filepath.Join(tools, "fzq")); err != nil {
		t.Skipf("fzq missing in %s", tools)
	}
	t.Setenv("VOX_LLM_API_KEY", "")
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools.Dir = tools
	cfg.Tools.FzqPath = filepath.Join(tools, "fzq")
	cfg.Tools.JqPath = filepath.Join(tools, "jq")
	cfg.Tools.YqPath = filepath.Join(tools, "yq")
	cfg.Media.Folders = nil
	cfg.Media.SyncOnStart = false
	if mod != nil {
		mod(cfg)
	}
	a, err := Build(cfg, WithLogger(logx.New("error", io.Discard)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Background syncs write into the data dir: let them finish before
		// the TempDir cleanup removes it.
		deadline := time.Now().Add(15 * time.Second)
		for idle := 0; idle < 5 && time.Now().Before(deadline); {
			if a.Syncer.Status().Running {
				idle = 0
			} else {
				idle++
			}
			time.Sleep(20 * time.Millisecond)
		}
		a.Close()
	})
	return a, dir
}

func musicDir(t *testing.T, names ...string) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "music")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(d, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func strp(s string) *string { return &s }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateMediaValidation(t *testing.T) {
	a, dir := newTestApp(t, nil)
	good := musicDir(t, "a - b.mp3")
	file := filepath.Join(good, "a - b.mp3")
	missing := filepath.Join(good, "nope")
	ctx := context.Background()

	_, err := a.UpdateMedia(ctx, MediaUpdate{Folders: []string{"relative/dir", missing, file, good}})
	var ve *MediaValidationError
	if !errors.As(err, &ve) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("want MediaValidationError, got %v", err)
	}
	for k, want := range map[string]string{
		"relative/dir": "absolute", missing: "does not exist", file: "not a directory",
	} {
		if got := ve.Fields[k]; !strings.Contains(got, want) {
			t.Errorf("field %q = %q, want %q", k, got, want)
		}
	}
	if _, bad := ve.Fields[good]; bad || len(ve.Fields) != 3 {
		t.Errorf("fields: %v", ve.Fields)
	}
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		raw, _ := os.ReadFile(filepath.Join(dir, config.FileName))
		if strings.Contains(string(raw), good) {
			t.Fatal("invalid update was persisted")
		}
	}

	// outside TRIM roots
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "/nonexistent-root:"+filepath.Join(t.TempDir(), "x"))
	_, err = a.UpdateMedia(ctx, MediaUpdate{Folders: []string{good}})
	if !errors.As(err, &ve) || !strings.Contains(ve.Fields[good], "accessible") {
		t.Fatalf("outside roots: %v", err)
	}
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", filepath.Dir(good))
	if _, err := a.UpdateMedia(ctx, MediaUpdate{Folders: []string{good}}); err != nil {
		t.Fatalf("inside roots: %v", err)
	}
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")

	// list-level problems
	many := make([]string, 0, MaxMediaFolders+1)
	for i := 0; i <= MaxMediaFolders; i++ {
		many = append(many, filepath.Join(t.TempDir(), "d"))
		_ = os.MkdirAll(many[i], 0o755)
	}
	if _, err = a.UpdateMedia(ctx, MediaUpdate{Folders: many}); !errors.As(err, &ve) || ve.Fields["folders"] == "" {
		t.Fatalf("too many: %v", err)
	}
	_, err = a.UpdateMedia(ctx, MediaUpdate{Folders: nil, Extensions: []string{"mp3", "b/ad"}, SyncInterval: strp("soon")})
	if !errors.As(err, &ve) || ve.Fields["extensions"] == "" || ve.Fields["sync_interval"] == "" {
		t.Fatalf("ext/interval: %v", err)
	}
	if _, err = a.UpdateMedia(ctx, MediaUpdate{SyncInterval: strp("5s")}); !errors.As(err, &ve) {
		t.Fatalf("too small interval: %v", err)
	}
}

func TestUpdateMediaPersistAndApply(t *testing.T) {
	a, dir := newTestApp(t, nil)
	cfgPath := filepath.Join(dir, config.FileName)
	if err := os.WriteFile(cfgPath, []byte("# my comment\nllm:\n  model: keep-me # inline\nmedia:\n  folders: []\n  max_files: 77 # cap\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m1 := musicDir(t, "A - one.mp3", "B - two.FLAC", "notes.txt")
	m2 := musicDir(t, "C - three.mp3")
	ctx := context.Background()

	started, err := a.UpdateMedia(ctx, MediaUpdate{
		Folders:      []string{m1 + "/", m1, m2}, // cleaned + deduped
		Extensions:   []string{".MP3", "flac", "mp3"},
		SyncInterval: strp("2h"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = started
	waitFor(t, "first sync", func() bool { return a.Library.Len() == 3 && !a.Syncer.Status().Running })

	// persisted, comments kept, other keys untouched, mode 0600
	raw, _ := os.ReadFile(cfgPath)
	for _, want := range []string{"# my comment", "# inline", "# cap", "max_files: 77", "keep-me"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("config.yaml lost %q:\n%s", want, raw)
		}
	}
	if fi, _ := os.Stat(cfgPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, raw)
	}
	if got := loaded.Media.Folders; len(got) != 2 || got[0] != m1 || got[1] != m2 {
		t.Errorf("folders %v", got)
	}
	if got := loaded.Media.Extensions; len(got) != 2 || got[0] != "mp3" || got[1] != "flac" {
		t.Errorf("extensions %v", got)
	}
	if loaded.Media.SyncInterval != 2*time.Hour || loaded.Media.MaxFiles != 77 {
		t.Errorf("interval/max_files %+v", loaded.Media)
	}
	live := a.MediaConfig()
	if len(live.Folders) != 2 || live.SyncInterval != 2*time.Hour || a.Scheduler.Interval != 2*time.Hour {
		t.Errorf("live %+v sched %v", live, a.Scheduler.Interval)
	}

	// extensions/interval nil = unchanged; shrink to one folder; "0" = manual
	if _, err := a.UpdateMedia(ctx, MediaUpdate{Folders: []string{m2}, SyncInterval: strp("0")}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second sync", func() bool { return a.Library.Len() == 1 && !a.Syncer.Status().Running })
	raw, _ = os.ReadFile(cfgPath)
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if mm := doc["media"].(map[string]any); mm["sync_interval"] != 0 {
		t.Errorf("sync_interval %#v", mm["sync_interval"])
	}
	loaded, err = config.Load(dir)
	if err != nil || loaded.Media.SyncInterval != 0 || len(loaded.Media.Extensions) != 2 {
		t.Fatalf("%v %+v", err, loaded.Media)
	}
	if a.Scheduler.Interval != 0 {
		t.Errorf("scheduler interval %v", a.Scheduler.Interval)
	}

	// empty list clears the library
	if _, err := a.UpdateMedia(ctx, MediaUpdate{Folders: []string{}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "clearing sync", func() bool { return a.Library.Len() == 0 && !a.Syncer.Status().Running })
	if got := a.MediaConfig().Folders; len(got) != 0 {
		t.Errorf("folders %v", got)
	}
	if st := a.Syncer.Status(); st.Result != "ok" || st.Tracks != 0 {
		t.Errorf("status %+v", st)
	}
}
