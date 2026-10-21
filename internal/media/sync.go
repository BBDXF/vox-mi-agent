package media

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/toolrunner"
)

// Settings keys.
const (
	SettingSnapshot = "media.snapshot"
	SettingStatus   = "media.status"
)

// Sync results.
const (
	ResultOK        = "ok"
	ResultUnchanged = "unchanged"
	ResultError     = "error"
)

// KeepCaches is how many fzq cache files survive pruning.
const KeepCaches = 2

// prebuildTimeout bounds the cache-building fzq run (100k tracks ~ seconds).
const prebuildTimeout = 2 * time.Minute

// ErrRunning is returned by TriggerAsync while a sync is in progress.
var ErrRunning = errors.New("media: sync already running")

// Store is the subset of *store.Store the syncer needs.
type Store interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	Audit(ctx context.Context, kind, actor string, detail any) error
}

// Status is the last known sync state (also stored in settings media.status).
type Status struct {
	Running    bool      `json:"running"`
	LastSyncAt time.Time `json:"last_sync_at"`
	Tracks     int       `json:"tracks"`
	DurationMs int64     `json:"duration_ms"`
	Result     string    `json:"result"` // ok | unchanged | error | ""
	Error      string    `json:"error,omitempty"`
	Warning    string    `json:"warning,omitempty"` // e.g. max_files truncation
}

// Deps of a Syncer. Only Runner is effectively required for real runs.
type Deps struct {
	Runner  *toolrunner.Runner // nil => toolrunner.New()
	Store   Store              // nil => status kept in memory only, no audit
	Aliases AliasStore         // nil => no aliases
	Library *Library           // reloaded after every sync; nil => created
	Log     *slog.Logger       // nil => slog.Default()
}

// Syncer is the single-flight library sync job.
type Syncer struct {
	cfg     *config.Config
	runner  *toolrunner.Runner
	store   Store
	aliases AliasStore
	lib     *Library
	log     *slog.Logger

	mu      sync.Mutex
	flight  *flight
	status  Status
	memSnap string // snapshot when no Store is configured
}

type flight struct {
	done chan struct{}
	st   Status
	err  error
}

// NewSyncer creates a syncer for cfg (Media, Search, Tools, DataDir are used).
func NewSyncer(cfg *config.Config, d Deps) *Syncer {
	s := &Syncer{cfg: cfg, runner: d.Runner, store: d.Store, aliases: d.Aliases, lib: d.Library, log: d.Log}
	if s.runner == nil {
		s.runner = toolrunner.New()
	}
	if s.lib == nil {
		s.lib = NewLibrary(SongsPath(cfg.DataDir))
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	_ = s.lib.Reload() // best effort: serve play_audio from the last sync
	if s.store != nil {
		if v, err := s.store.GetSetting(context.Background(), SettingStatus); err == nil {
			var st Status
			if json.Unmarshal([]byte(v), &st) == nil {
				st.Running = false
				s.status = st
			}
		}
	}
	return s
}

// Library returns the library reloaded after each sync.
func (s *Syncer) Library() *Library { return s.lib }

// Status returns a copy of the current status.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Running = s.flight != nil
	return st
}

// Trigger runs a sync and waits for it. If one is already running, the call
// joins it and returns its result. source ("start", "timer", "manual", ...)
// is recorded in the audit log.
func (s *Syncer) Trigger(ctx context.Context, source string) (Status, error) {
	s.mu.Lock()
	if f := s.flight; f != nil {
		s.mu.Unlock()
		select {
		case <-f.done:
			return f.st, f.err
		case <-ctx.Done():
			return Status{}, ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	s.flight = f
	s.mu.Unlock()
	return s.lead(ctx, f, source)
}

// TriggerAsync starts a sync in the background; ErrRunning if one is active.
func (s *Syncer) TriggerAsync(source string) error {
	s.mu.Lock()
	if s.flight != nil {
		s.mu.Unlock()
		return ErrRunning
	}
	f := &flight{done: make(chan struct{})}
	s.flight = f
	s.mu.Unlock()
	go s.lead(context.Background(), f, source) //nolint:errcheck
	return nil
}

func (s *Syncer) lead(ctx context.Context, f *flight, source string) (Status, error) {
	start := time.Now()
	st, err := s.run(ctx)
	st.DurationMs = time.Since(start).Milliseconds()
	st.LastSyncAt = time.Now().UTC()
	if err != nil {
		st.Result = ResultError
		st.Error = err.Error()
		s.log.Error("media sync failed", "err", err)
	}
	st.Running = false

	s.mu.Lock()
	s.status = st
	f.st, f.err = st, err
	s.flight = nil
	s.mu.Unlock()
	close(f.done)

	if s.store != nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if b, merr := json.Marshal(st); merr == nil {
			if e := s.store.SetSetting(sctx, SettingStatus, string(b)); e != nil {
				s.log.Warn("media: save status", "err", e)
			}
		}
		if e := s.store.Audit(sctx, "media.sync", source, st); e != nil {
			s.log.Warn("media: audit", "err", e)
		}
	}
	return st, err
}

// fileEntry is one scanned audio file.
type fileEntry struct {
	folder int
	rel    string // slash-separated, relative to folder root
	abs    string
	size   int64
	mtime  int64 // unix nanos
}

// checkFolder enforces: absolute, exists, is a directory, and (when
// TRIM_DATA_ACCESSIBLE_PATHS is set) lies under one of its ':'-separated paths.
func checkFolder(folder string) error {
	if !filepath.IsAbs(folder) {
		return fmt.Errorf("media folder %q is not an absolute path", folder)
	}
	clean := filepath.Clean(folder)
	if env := os.Getenv("TRIM_DATA_ACCESSIBLE_PATHS"); env != "" {
		ok := false
		for _, p := range strings.Split(env, ":") {
			if p = strings.TrimSpace(p); p == "" {
				continue
			}
			rel, err := filepath.Rel(filepath.Clean(p), clean)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("media folder %q is outside TRIM_DATA_ACCESSIBLE_PATHS", folder)
		}
	}
	fi, err := os.Stat(clean)
	if err != nil {
		return fmt.Errorf("media folder %q: %w", folder, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("media folder %q is not a directory", folder)
	}
	return nil
}

// scan walks the folders: symlinks are never followed, extensions are matched
// case-insensitively, at most max files are returned (truncated reports a cut).
func scan(folders, exts []string, max int) (files []fileEntry, truncated bool, err error) {
	want := map[string]bool{}
	for _, e := range exts {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			want["."+e] = true
		}
	}
	for idx, folder := range folders {
		// The configured root itself may be a symlink (mount points); only
		// links found inside it are ignored.
		root, e := filepath.EvalSymlinks(filepath.Clean(folder))
		if e != nil {
			return nil, false, fmt.Errorf("media folder %q: %w", folder, e)
		}
		werr := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				if p == root {
					return werr
				}
				return nil // unreadable subtree: skip
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), "._") {
				return nil
			}
			if !want[strings.ToLower(filepath.Ext(d.Name()))] {
				return nil
			}
			if max > 0 && len(files) >= max {
				truncated = true
				return fs.SkipAll
			}
			info, ierr := d.Info()
			if ierr != nil {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			files = append(files, fileEntry{
				folder: idx, rel: filepath.ToSlash(rel), abs: filepath.Join(filepath.Clean(folder), rel),
				size: info.Size(), mtime: info.ModTime().UnixNano(),
			})
			return nil
		})
		if werr != nil {
			return nil, false, werr
		}
		if truncated {
			break
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].abs < files[j].abs })
	return files, truncated, nil
}

// snapshotHash covers every input of the songs file: relpath+size+mtime of all
// files, the filename pattern, and the alias table.
func snapshotHash(files []fileEntry, pattern string, aliases map[string][]string) string {
	h := sha1.New()
	for _, f := range files {
		fmt.Fprintf(h, "%d\x00%s\x00%d\x00%d\n", f.folder, f.rel, f.size, f.mtime)
	}
	fmt.Fprintf(h, "pattern\x00%s\n", pattern)
	titles := make([]string, 0, len(aliases))
	for t := range aliases {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	for _, t := range titles {
		fmt.Fprintf(h, "alias\x00%s\x00%s\n", t, strings.Join(aliases[t], "\x01"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// parseName extracts artist/title from the relative path via the pattern's
// named groups; title falls back to the basename without extension.
func parseName(re *regexp.Regexp, rel string) (artist, title string) {
	if re != nil {
		if m := re.FindStringSubmatch(rel); m != nil {
			for i, n := range re.SubexpNames() {
				switch n {
				case "artist":
					artist = strings.TrimSpace(m[i])
				case "title":
					title = strings.TrimSpace(m[i])
				}
			}
		}
	}
	if title == "" {
		artist = ""
		b := path.Base(rel)
		title = strings.TrimSuffix(b, path.Ext(b))
	}
	return artist, title
}

func (s *Syncer) run(ctx context.Context) (Status, error) {
	var st Status
	mc := s.cfg.Media
	if len(mc.Folders) == 0 {
		return st, errors.New("media: no folders configured")
	}
	for _, f := range mc.Folders {
		if err := checkFolder(f); err != nil {
			return st, err
		}
	}
	var re *regexp.Regexp
	if mc.FilenamePattern != "" {
		var err error
		if re, err = regexp.Compile(mc.FilenamePattern); err != nil {
			return st, fmt.Errorf("media: filename_pattern: %w", err)
		}
	}

	// 1. scan + compare snapshot
	files, truncated, err := scan(mc.Folders, mc.Extensions, mc.MaxFiles)
	if err != nil {
		return st, err
	}
	if truncated {
		st.Warning = fmt.Sprintf("library truncated at max_files=%d", mc.MaxFiles)
		s.log.Warn("media: " + st.Warning)
	}
	var aliases map[string][]string
	if s.aliases != nil {
		if aliases, err = s.aliases.Aliases(ctx); err != nil {
			return st, fmt.Errorf("media: aliases: %w", err)
		}
	}
	snap := snapshotHash(files, mc.FilenamePattern, aliases)
	songs := SongsPath(s.cfg.DataDir)
	if s.store != nil {
		old, gerr := s.store.GetSetting(ctx, SettingSnapshot)
		if _, serr := os.Stat(songs); gerr == nil && old == snap && serr == nil {
			if lerr := s.lib.Reload(); lerr != nil {
				return st, lerr
			}
			st.Result = ResultUnchanged
			st.Tracks = len(files)
			return st, nil
		}
	} else if s.unchangedLocked(snap, songs) {
		if lerr := s.lib.Reload(); lerr != nil {
			return st, lerr
		}
		st.Result = ResultUnchanged
		st.Tracks = len(files)
		return st, nil
	}

	// 2. generate
	tracks := make([]Track, 0, len(files))
	for _, f := range files {
		artist, title := parseName(re, f.rel)
		t := Track{
			ID: TrackID(f.folder, f.rel), Title: title, Artist: artist,
			Alias: []string{}, Path: f.abs,
		}
		if a := aliases[title]; len(a) > 0 {
			t.Alias = append([]string(nil), a...)
		}
		tracks = append(tracks, t)
	}

	// 3. atomic write
	if err := writeSongs(songs, tracks); err != nil {
		return st, err
	}
	if err := s.lib.Reload(); err != nil {
		return st, err
	}
	st.Tracks = len(tracks)

	// 4. prebuild cache, 5. prune
	if err := s.prebuild(ctx, songs); err != nil {
		return st, err
	}

	// snapshot is saved only once everything succeeded
	if s.store != nil {
		if err := s.store.SetSetting(ctx, SettingSnapshot, snap); err != nil {
			return st, fmt.Errorf("media: save snapshot: %w", err)
		}
	} else {
		s.mu.Lock()
		s.memSnap = snap
		s.mu.Unlock()
	}
	st.Result = ResultOK
	return st, nil
}

func (s *Syncer) unchangedLocked(snap, songs string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(songs); err != nil {
		return false
	}
	return s.memSnap == snap
}

// writeSongs writes tracks (already sorted) to path atomically.
func writeSongs(dst string, tracks []Track) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	for _, t := range tracks {
		if t.Alias == nil {
			t.Alias = []string{}
		}
		if err := enc.Encode(t); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(filepath.Dir(dst)); err == nil { // best effort: persist the rename
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// prebuild runs fzq once with the exact query-time index args and an empty
// query so the index cache exists, then prunes old cache files.
func (s *Syncer) prebuild(ctx context.Context, songs string) error {
	cache := CacheDir(s.cfg.DataDir)
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return err
	}
	before := listFiles(cache)
	args := []string{"--queries", "-", "-o", "json", "-n", "1", "--index-cache", cache}
	args = append(args, IndexArgs(songs)...)
	res, err := s.runner.Run(ctx, toolrunner.Spec{
		Tool: "media_sync", Source: "media_library", Query: "(prebuild index cache)",
		Path: fzqPath(s.cfg), Args: args, Timeout: prebuildTimeout,
	})
	if err != nil {
		return fmt.Errorf("media: prebuild cache: %w", err)
	}
	if res.ExitCode != 0 && res.ExitCode != 1 { // 1 = no result, fine
		return fmt.Errorf("media: prebuild cache: fzq exit %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	fresh := map[string]bool{}
	for name := range listFiles(cache) {
		if _, had := before[name]; !had {
			fresh[name] = true
		}
	}
	if len(listFiles(cache)) == 0 {
		return errors.New("media: prebuild cache: fzq produced no cache file")
	}
	pruneCache(cache, KeepCaches, fresh)
	return nil
}

func listFiles(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil {
			out[e.Name()] = info.ModTime()
		}
	}
	return out
}

// pruneCache keeps the newest `keep` regular files (files in protect are
// always kept and count toward the quota) and removes the rest.
func pruneCache(dir string, keep int, protect map[string]bool) {
	files := listFiles(dir)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := files[names[i]], files[names[j]]
		if !a.Equal(b) {
			return a.After(b)
		}
		return names[i] > names[j]
	})
	kept := 0
	for n := range protect {
		if _, ok := files[n]; ok {
			kept++
		}
	}
	for _, n := range names {
		if protect[n] {
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		_ = os.Remove(filepath.Join(dir, n))
	}
}
