package media

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// Library is the in-memory id -> Track map loaded from songs.jsonl, used by
// play_audio to find the file path of an id.
type Library struct {
	path string
	mu   sync.RWMutex
	byID map[string]Track
}

// NewLibrary creates a library reading path. Call Reload to load it.
func NewLibrary(songsPath string) *Library {
	return &Library{path: songsPath, byID: map[string]Track{}}
}

// Reload (re)reads the songs file. A missing file yields an empty library.
func (l *Library) Reload() error {
	f, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			l.mu.Lock()
			l.byID = map[string]Track{}
			l.mu.Unlock()
			return nil
		}
		return err
	}
	defer f.Close()
	m := map[string]Track{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	n := 0
	for sc.Scan() {
		n++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var t Track
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return fmt.Errorf("media: %s line %d: %w", l.path, n, err)
		}
		m[t.ID] = t
	}
	if err := sc.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	l.byID = m
	l.mu.Unlock()
	return nil
}

// Lookup returns the track with the given id.
func (l *Library) Lookup(id string) (Track, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	t, ok := l.byID[id]
	return t, ok
}

// Len is the number of loaded tracks.
func (l *Library) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.byID)
}
