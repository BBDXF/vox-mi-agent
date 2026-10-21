package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// AccessibleRootsEnv restricts where media folders may live (fnOS sets it).
const AccessibleRootsEnv = "TRIM_DATA_ACCESSIBLE_PATHS"

// AccessibleRoots returns the cleaned ':'-separated paths of
// TRIM_DATA_ACCESSIBLE_PATHS, or nil when it is unset/empty.
func AccessibleRoots() []string {
	var roots []string
	for _, p := range strings.Split(os.Getenv(AccessibleRootsEnv), ":") {
		if p = strings.TrimSpace(p); p != "" {
			roots = append(roots, filepath.Clean(p))
		}
	}
	return roots
}

// UnderRoots reports whether the clean absolute path p equals or lies below
// one of roots.
func UnderRoots(roots []string, p string) bool {
	for _, r := range roots {
		rel, err := filepath.Rel(r, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ValidateFolder checks that folder is an absolute, existing, readable
// directory (within the accessible roots when configured). It returns the
// cleaned path, or a short human-readable reason as the error.
func ValidateFolder(folder string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", errors.New("path is empty")
	}
	if !filepath.IsAbs(folder) {
		return "", errors.New("must be an absolute path")
	}
	clean := filepath.Clean(folder)
	if roots := AccessibleRoots(); len(roots) > 0 && !UnderRoots(roots, clean) {
		return "", errors.New("outside the accessible paths (" + AccessibleRootsEnv + ")")
	}
	fi, err := os.Stat(clean)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", errors.New("does not exist")
	case errors.Is(err, fs.ErrPermission):
		return "", errors.New("permission denied")
	case err != nil:
		return "", fmt.Errorf("cannot stat: %v", err)
	case !fi.IsDir():
		return "", errors.New("not a directory")
	}
	if _, err := os.ReadDir(clean); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return "", errors.New("permission denied")
		}
		return "", fmt.Errorf("not readable: %v", err)
	}
	return clean, nil
}

// NormalizeExtensions lowercases, strips leading dots, drops duplicates and
// returns an error for an entry that is not [a-z0-9]+.
func NormalizeExtensions(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e == "" {
			return nil, errors.New("empty extension")
		}
		for _, c := range e {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return nil, fmt.Errorf("invalid extension %q (letters and digits only)", e)
			}
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

// CountAudioFiles counts regular files with one of exts (without dots,
// case-insensitive) below dir without following symlinks, stopping once
// limit is reached (capped is then true). Hidden "._" files are ignored like
// in the real scan. A cancelled ctx ends the walk early (capped = true).
func CountAudioFiles(ctx context.Context, dir string, exts []string, limit int) (n int, capped bool, err error) {
	want := map[string]bool{}
	for _, e := range exts {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			want["."+e] = true
		}
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return 0, false, err
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if ctx.Err() != nil { // out of time: report what we have as a lower bound
			capped = true
			return fs.SkipAll
		}
		if werr != nil {
			if p == root {
				return werr
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), "._") || !want[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		n++
		if limit > 0 && n >= limit {
			capped = true
			return fs.SkipAll
		}
		return nil
	})
	return n, capped, err
}
