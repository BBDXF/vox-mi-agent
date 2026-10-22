package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
)

func toolsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("VOX_TOOLS_DIR")
	if dir == "" {
		dir, _ = filepath.Abs("../../.tools")
	}
	if _, err := os.Stat(filepath.Join(dir, "fzq")); err != nil {
		t.Skipf("fzq missing in %s", dir)
	}
	return dir
}

type testEnv struct {
	cfg     *config.Config
	music   string
	store   *store.Store
	runner  *toolrunner.Runner
	aliases *MemAliasStore
	sync    *Syncer
	index   *Index
	lib     *Library
}

// newEnv creates a music folder with empty files (relative slash paths).
func newEnv(t *testing.T, files []string, mod func(*config.Config)) *testEnv {
	t.Helper()
	tools := toolsDir(t)
	root := t.TempDir()
	music := filepath.Join(root, "music")
	for _, f := range files {
		touch(t, filepath.Join(music, filepath.FromSlash(f)))
	}
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.Tools.Dir = tools
	cfg.Media.Folders = []string{music}
	if mod != nil {
		mod(cfg)
	}
	st, err := store.Open(filepath.Join(root, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := toolrunner.New()
	r.WorkDir = filepath.Join(root, "toolrun")
	al := NewMemAliasStore(nil)
	s := NewSyncer(cfg, Deps{Runner: r, Store: st, Aliases: al})
	return &testEnv{cfg: cfg, music: music, store: st, runner: r, aliases: al, sync: s, index: NewIndex(cfg, r), lib: s.Library()}
}

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) trigger(t *testing.T) Status {
	t.Helper()
	st, err := e.sync.Trigger(context.Background(), "test")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return st
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}
