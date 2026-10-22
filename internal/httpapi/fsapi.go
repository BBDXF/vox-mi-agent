package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/media"
)

const (
	maxDirEntries   = 500
	maxCountFiles   = 2000
	countTimeBudget = 5 * time.Second
)

type dirEntryDTO struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Symlink bool   `json:"symlink,omitempty"`
}

// resolveRoots returns the symlink-resolved accessible roots (unresolvable
// roots are kept as is).
func resolveRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if rr, err := filepath.EvalSymlinks(r); err == nil {
			r = rr
		}
		out = append(out, r)
	}
	return out
}

// allowedPath enforces TRIM_DATA_ACCESSIBLE_PATHS (when set) on a clean
// absolute path, both lexically and after resolving symlinks.
func allowedPath(roots []string, p string) bool {
	if len(roots) == 0 {
		return true
	}
	if !media.UnderRoots(roots, p) {
		return false
	}
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		return media.UnderRoots(resolveRoots(roots), rp) || media.UnderRoots(roots, rp)
	}
	return true
}

// GET /api/fs/dirs?path=/abs: directory browser for the media folder picker.
func (s *Server) fsDirs(w http.ResponseWriter, r *http.Request) {
	roots := media.AccessibleRoots()
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		p = "/"
		if len(roots) > 0 {
			p = roots[0]
		}
	}
	if !filepath.IsAbs(p) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	p = filepath.Clean(p)
	if !allowedPath(roots, p) {
		writeError(w, http.StatusForbidden, "path is outside the accessible paths")
		return
	}
	fi, err := os.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, "path does not exist")
		return
	case errors.Is(err, fs.ErrPermission):
		writeError(w, http.StatusForbidden, "permission denied")
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "cannot access path")
		return
	case !fi.IsDir():
		writeError(w, http.StatusBadRequest, "not a directory")
		return
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			writeError(w, http.StatusForbidden, "permission denied")
		} else {
			writeError(w, http.StatusBadRequest, "cannot read directory")
		}
		return
	}

	entries := make([]dirEntryDTO, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(p, name)
		link := e.Type()&fs.ModeSymlink != 0
		if link {
			st, err := os.Stat(full) // follows the link
			if err != nil || !st.IsDir() || !allowedPath(roots, full) {
				continue
			}
		} else if !e.IsDir() {
			continue
		}
		entries = append(entries, dirEntryDTO{Name: name, Path: full, Symlink: link})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a != b {
			return a < b
		}
		return entries[i].Name < entries[j].Name
	})
	truncated := false
	if len(entries) > maxDirEntries {
		entries, truncated = entries[:maxDirEntries], true
	}

	var parent any // null at "/" and at an accessible root
	if up := filepath.Dir(p); up != p && (len(roots) == 0 || media.UnderRoots(roots, up)) {
		parent = up
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": p, "parent": parent, "entries": entries, "truncated": truncated,
	})
}

// POST /api/fs/validate {path}: quick check of a candidate media folder.
func (s *Server) fsValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	clean, err := media.ValidateFolder(body.Path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": err.Error()})
		return
	}
	ctx, cancel := contextWithTimeout(r, countTimeBudget)
	defer cancel()
	n, capped, err := media.CountAudioFiles(ctx, clean, s.app.MediaConfig().Extensions, maxCountFiles)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "reason": "cannot scan: " + err.Error()})
		return
	}
	resp := map[string]any{"ok": true, "audio_files_estimate": n, "audio_files_capped": capped}
	if capped {
		resp["audio_files_label"] = itoa(n) + "+"
	} else {
		resp["audio_files_label"] = itoa(n)
	}
	writeJSON(w, http.StatusOK, resp)
}

func itoa(n int) string { return strconv.Itoa(n) }

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
