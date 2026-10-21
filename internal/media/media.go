// Package media owns the local music library: scanning folders, writing the
// songs list, prebuilding the fzq index cache, and searching it
// (PROJECT_DESIGN.md §6.4.2).
package media

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/andy/vox-mi-agent/internal/config"
)

// Track is one song of the library. Path is server-side only: it is never
// returned to the LLM.
type Track struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Artist string   `json:"artist"`
	Album  string   `json:"album"`
	Alias  []string `json:"alias"`
	Path   string   `json:"path"`
}

// Hit is one search result as exposed to the agent (no path).
type Hit struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Artist string  `json:"artist"`
	Score  float64 `json:"score"`
}

// TrackID is the stable id of a file: first 12 hex chars of
// sha1("<folderIndex>/<relative slash path>").
func TrackID(folderIdx int, rel string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d/%s", folderIdx, filepath.ToSlash(rel))))
	return hex.EncodeToString(sum[:])[:12]
}

// IndexArgs is the SINGLE source of the fzq arguments that influence the
// index (and thus the cache key). Both the sync job (cache prebuild) and the
// query path use it, so the cache always hits. The songs file is the last
// element (positional argument) and must stay last.
func IndexArgs(songsFile string) []string {
	return []string{"-f", "jsonl", "-k", "title", "-k", "artist", "-k", "alias", songsFile}
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
