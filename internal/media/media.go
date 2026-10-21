// Package media owns the local music library: scanning folders, writing the
// songs list, prebuilding the fzq index cache, and searching it
// (PROJECT_DESIGN.md §6.4.2).
package media

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/andy/vox-mi-agent/internal/config"
)

// Track is one song of the library. Path is server-side only: it is never
// returned to the LLM.
type Track struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"` // original joined string, for display
	// Artists is Artist split on the multi-artist separators (SplitArtists);
	// fzq matches any of them.
	Artists []string `json:"artists"`
	Album   string   `json:"album"`
	Year    int      `json:"year,omitempty"`
	TrackNo int      `json:"track_no,omitempty"`
	Genre   string   `json:"genre,omitempty"`
	// TagSource tells where title/artist came from: filename | tags | mixed.
	TagSource string   `json:"tag_source"`
	Alias     []string `json:"alias"`
	Path      string   `json:"path"`
}

// Hit is one search result as exposed to the agent (no path).
type Hit struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Score  float64 `json:"score"`
}

// TrackID is the stable id of a file: first 12 hex chars of
// sha1(<cleaned absolute slash path>). It does not depend on folder order;
// it is the key used by play_audio, traces and media URLs (/audio/{id}.{ext}).
func TrackID(absPath string) string {
	return trackHash(absPath)[:idLen]
}

// idLen is the normal track id length (48 bits; ~1.8e-5 chance of any
// collision among 100k files).
const idLen = 12

func trackHash(absPath string) string {
	sum := sha1.Sum([]byte(norm.NFC.String(filepath.ToSlash(filepath.Clean(absPath)))))
	return hex.EncodeToString(sum[:])
}

// uniqueTrackID returns TrackID(absPath) unless another path already holds
// it in taken; then it lengthens the prefix (16, 24, … 40 hex) until free.
// Files are processed in sorted path order, so the result is deterministic.
func uniqueTrackID(absPath string, taken map[string]string) string {
	h := trackHash(absPath)
	for n := idLen; n <= len(h); n += 4 {
		if p, ok := taken[h[:n]]; !ok || p == absPath {
			return h[:n]
		}
	}
	return h
}

// IndexArgs is the SINGLE source of the fzq arguments that influence the
// index (and thus the cache key). Both the sync job (cache prebuild) and the
// query path use it, so the cache always hits. The songs file is the last
// element (positional argument) and must stay last.
func IndexArgs(songsFile string) []string {
	return []string{"-f", "jsonl", "-k", "title", "-k", "artist", "-k", "artists[]", "-k", "alias", songsFile}
}

// SongsPath is <data>/media/songs.jsonl.
func SongsPath(dataDir string) string { return filepath.Join(dataDir, "media", "songs.jsonl") }

// CacheDir is <data>/cache/fzq.
func CacheDir(dataDir string) string { return filepath.Join(dataDir, "cache", "fzq") }

func fzqPath(cfg *config.Config) string {
	if cfg.Tools.FzqPath != "" {
		return cfg.Tools.FzqPath
	}
	dir := cfg.Tools.Dir
	if dir == "" {
		dir = config.DefaultToolsDir
	}
	return filepath.Join(dir, "fzq")
}

// artistSep matches the separators between several artists of one track:
// " _ ", "、", "&" (spaces optional), " / ", "," / "，" (spaces optional),
// " feat. ", " feat ", " ft. ", " ft ", and a standalone " x " (feat/ft/x are
// case-insensitive; "x" inside a word never splits).
var artistSep = regexp.MustCompile(`(?i)\s+_\s+|\s*、\s*|\s*&\s*|\s+/\s+|\s*[,，]\s*|\s+(?:feat|ft)\.?\s+|\s+x\s+`)

// SplitArtists splits a joined artist string into its individual artists
// (trimmed, empty and duplicate parts dropped, order kept). An empty input
// yields an empty (non-nil) slice.
func SplitArtists(artist string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range artistSep.Split(artist, -1) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
