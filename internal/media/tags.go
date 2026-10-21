package media

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dhowden/tag"
)

// Tag modes (media.tag_mode).
const (
	// TagFilenameFirst takes artist/title from the filename pattern when it
	// yields both, else from the embedded tags (default).
	TagFilenameFirst = "filename_first"
	// TagTagsFirst prefers the embedded title/artist, falling back to the
	// filename pattern per field.
	TagTagsFirst = "tags_first"
	// TagFilenameOnly never reads embedded tags.
	TagFilenameOnly = "filename_only"
)

// TagModes lists the valid tag_mode values.
var TagModes = []string{TagFilenameFirst, TagTagsFirst, TagFilenameOnly}

// ValidTagMode reports whether m is a known tag mode.
func ValidTagMode(m string) bool {
	for _, v := range TagModes {
		if v == m {
			return true
		}
	}
	return false
}

// Tag sources recorded on a Track.
const (
	SourceFilename = "filename"
	SourceTags     = "tags"
	SourceMixed    = "mixed"
)

// tagWorkers bounds the parallel tag readers.
const tagWorkers = 4

// rawTags is what was read from one file, independent of tag_mode (so that a
// mode change never needs a re-read). It doubles as a line of tags.jsonl,
// keyed by (Path, Size, MTime).
type rawTags struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	OK     bool   `json:"ok"` // false: unreadable / no tags
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	Year   int    `json:"year,omitempty"`
	Track  int    `json:"track_no,omitempty"`
	Genre  string `json:"genre,omitempty"`
}

// TagsPath is <data>/media/tags.jsonl, the per-file tag cache.
func TagsPath(dataDir string) string { return filepath.Join(dataDir, "media", "tags.jsonl") }

// readFileTags reads the embedded tags of one audio file.
func readFileTags(p string) (rt rawTags, err error) {
	defer func() {
		if r := recover(); r != nil { // tolerate parser panics on corrupt files
			rt, err = rawTags{}, fmt.Errorf("tag parser panic: %v", r)
		}
	}()
	f, err := os.Open(p)
	if err != nil {
		return rawTags{}, err
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return rawTags{}, err
	}
	rt.OK = true
	rt.Title = strings.TrimSpace(m.Title())
	rt.Artist = strings.TrimSpace(m.Artist())
	rt.Album = strings.TrimSpace(m.Album())
	rt.Genre = strings.TrimSpace(m.Genre())
	rt.Year = m.Year()
	rt.Track, _ = m.Track()
	if rt.Year < 0 {
		rt.Year = 0
	}
	if rt.Track < 0 {
		rt.Track = 0
	}
	return rt, nil
}

// loadTagCache loads tags.jsonl; a missing or damaged file yields an empty
// (or partial) cache.
func loadTagCache(p string) map[string]rawTags {
	out := map[string]rawTags{}
	f, err := os.Open(p)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var rt rawTags
		if json.Unmarshal(sc.Bytes(), &rt) == nil && rt.Path != "" {
			out[rt.Path] = rt
		}
	}
	return out
}

func saveTagCache(dst string, entries []rawTags) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	var werr error
	for _, e := range entries {
		if werr = enc.Encode(e); werr != nil {
			break
		}
	}
	if werr == nil {
		werr = bw.Flush()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, dst)
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

// collectTags returns the raw tags of every file (by abs path). Files whose
// (path,size,mtime) match the cache are not re-read; the rest are read by a
// bounded worker pool. Read errors are logged at debug level and recorded as
// OK=false (so the filename is used).
func (s *Syncer) collectTags(ctx context.Context, files []fileEntry, cache map[string]rawTags) (map[string]rawTags, error) {
	out := make(map[string]rawTags, len(files))
	var todo []fileEntry
	for _, f := range files {
		if c, ok := cache[f.abs]; ok && c.Size == f.size && c.MTime == f.mtime {
			out[f.abs] = c
			continue
		}
		todo = append(todo, f)
	}
	if len(todo) == 0 {
		return out, nil
	}
	read := s.readTags
	if read == nil {
		read = readFileTags
	}
	jobs := make(chan fileEntry)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(tagWorkers, len(todo)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				rt, err := read(f.abs)
				if err != nil {
					if !errors.Is(err, tag.ErrNoTagsFound) && !errors.Is(err, fs.ErrNotExist) {
						s.log.Debug("media: read tags failed, using filename", "path", f.abs, "err", err)
					}
					rt = rawTags{}
				}
				rt.Path, rt.Size, rt.MTime = f.abs, f.size, f.mtime
				mu.Lock()
				out[f.abs] = rt
				mu.Unlock()
			}
		}()
	}
	var cerr error
loop:
	for _, f := range todo {
		select {
		case jobs <- f:
		case <-ctx.Done():
			cerr = ctx.Err()
			break loop
		}
	}
	close(jobs)
	wg.Wait()
	return out, cerr
}

// pickNames chooses title/artist per tag_mode. rel-derived fnArtist/fnTitle
// come from parseName (fnMatched: the pattern matched with a title).
func pickNames(mode string, fnArtist, fnTitle string, fnMatched bool, rt rawTags) (artist, title, source string) {
	if mode == TagFilenameOnly || !rt.OK {
		return fnArtist, fnTitle, SourceFilename
	}
	var useFilename bool
	switch mode {
	case TagTagsFirst:
		useFilename = false
	default: // filename_first: a complete (artist+title) filename match wins
		useFilename = fnMatched && fnArtist != ""
	}
	if useFilename {
		return fnArtist, fnTitle, SourceFilename
	}
	title, artist = rt.Title, rt.Artist
	tTags, aTags := title != "", artist != ""
	if !tTags {
		title = fnTitle
	}
	if !aTags {
		artist = fnArtist
	}
	// source: what the displayed values came from
	fromTags := tTags || aTags
	fromName := !tTags || (!aTags && artist != "")
	switch {
	case fromTags && fromName:
		source = SourceMixed
	case fromTags:
		source = SourceTags
	default:
		source = SourceFilename
	}
	return artist, title, source
}
