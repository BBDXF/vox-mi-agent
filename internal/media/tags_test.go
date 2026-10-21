package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/andy/vox-mi-agent/internal/config"
)

type id3 struct {
	title, artist, album, year, track, genre string
}

// mp3Bytes builds a minimal ID3v2.3 tag (no audio frames needed for tag reads).
func mp3Bytes(m id3) []byte {
	var frames []byte
	text := func(id, v string) {
		if v == "" {
			return
		}
		body := append([]byte{0}, v...) // ISO-8859-1 / raw bytes
		hdr := make([]byte, 10)
		copy(hdr, id)
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(body)))
		frames = append(frames, hdr...)
		frames = append(frames, body...)
	}
	text("TIT2", m.title)
	text("TPE1", m.artist)
	text("TALB", m.album)
	text("TYER", m.year)
	text("TRCK", m.track)
	text("TCON", m.genre)
	n := len(frames)
	out := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(append(out, frames...), make([]byte, 64)...)
}

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func trackByTitleOrPath(t *testing.T, e *testEnv, base string) Track {
	t.Helper()
	for _, tr := range e.lib.Tracks() {
		if filepath.Base(tr.Path) == base {
			return tr
		}
	}
	t.Fatalf("no track for %s", base)
	return Track{}
}

func tagFixtureEnv(t *testing.T, mode string) *testEnv {
	t.Helper()
	e := newEnv(t, []string{"Name A - Song A.mp3", "plain.mp3", "untagged - Thing.mp3"}, func(c *config.Config) { c.Media.TagMode = mode })
	writeFile(t, filepath.Join(e.music, "Name A - Song A.mp3"), mp3Bytes(id3{
		title: "Tag Title", artist: "Tag One & Tag Two", album: "The Album", year: "2020", track: "3/12", genre: "Rock",
	}))
	writeFile(t, filepath.Join(e.music, "plain.mp3"), mp3Bytes(id3{title: "Only Tags", artist: "Solo"}))
	return e
}

func TestTagModes(t *testing.T) {
	cases := []struct {
		mode                              string
		titleA, artistA, srcA             string
		titlePlain, artistPlain, srcPlain string
		album                             string
		year, trackNo                     int
	}{
		{TagFilenameFirst, "Song A", "Name A", SourceFilename, "Only Tags", "Solo", SourceTags, "The Album", 2020, 3},
		{TagTagsFirst, "Tag Title", "Tag One & Tag Two", SourceTags, "Only Tags", "Solo", SourceTags, "The Album", 2020, 3},
		{TagFilenameOnly, "Song A", "Name A", SourceFilename, "plain", "", SourceFilename, "", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			e := tagFixtureEnv(t, c.mode)
			if st := e.trigger(t); st.Result != ResultOK || st.Tracks != 3 {
				t.Fatalf("%+v", st)
			}
			a := trackByTitleOrPath(t, e, "Name A - Song A.mp3")
			if a.Title != c.titleA || a.Artist != c.artistA || a.TagSource != c.srcA {
				t.Fatalf("A: %+v", a)
			}
			if a.Album != c.album || a.Year != c.year || a.TrackNo != c.trackNo {
				t.Fatalf("A extras: %+v", a)
			}
			if c.mode != TagFilenameOnly && a.Genre != "Rock" {
				t.Fatalf("genre %q", a.Genre)
			}
			if c.mode == TagTagsFirst {
				if len(a.Artists) != 2 || a.Artists[0] != "Tag One" || a.Artists[1] != "Tag Two" {
					t.Fatalf("artists split of winning artist: %v", a.Artists)
				}
			}
			p := trackByTitleOrPath(t, e, "plain.mp3")
			if p.Title != c.titlePlain || p.Artist != c.artistPlain || p.TagSource != c.srcPlain {
				t.Fatalf("plain: %+v", p)
			}
			// untagged file (read error) falls back to the filename in every mode
			u := trackByTitleOrPath(t, e, "untagged - Thing.mp3")
			if u.Title != "Thing" || u.Artist != "untagged" || u.TagSource != SourceFilename {
				t.Fatalf("untagged: %+v", u)
			}
		})
	}
}

func TestTagModeChangesSnapshot(t *testing.T) {
	files := []fileEntry{{abs: "/m/a.mp3", rel: "a.mp3", size: 1, mtime: 2}}
	a := snapshotHash(files, "p", TagFilenameFirst, nil)
	if a == snapshotHash(files, "p", TagTagsFirst, nil) {
		t.Fatal("tag_mode must be part of the snapshot")
	}
}

type countingReader struct {
	mu    sync.Mutex
	reads map[string]int
}

func (c *countingReader) read(p string) (rawTags, error) {
	c.mu.Lock()
	c.reads[filepath.Base(p)]++
	c.mu.Unlock()
	return readFileTags(p)
}

func (c *countingReader) reset() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.reads {
		n += v
	}
	c.reads = map[string]int{}
	return n
}

func TestTagCacheReuse(t *testing.T) {
	e := tagFixtureEnv(t, TagTagsFirst)
	cr := &countingReader{reads: map[string]int{}}
	e.sync.readTags = cr.read

	e.trigger(t)
	if n := cr.reset(); n != 3 {
		t.Fatalf("first sync read %d files, want 3", n)
	}
	if _, err := os.Stat(TagsPath(e.cfg.DataDir)); err != nil {
		t.Fatalf("tags cache not written: %v", err)
	}

	// a new file: only it is read
	writeFile(t, filepath.Join(e.music, "new.mp3"), mp3Bytes(id3{title: "New", artist: "N"}))
	e.trigger(t)
	if n := cr.reset(); n != 1 {
		t.Fatalf("after adding a file read %d, want 1", n)
	}
	if tr := trackByTitleOrPath(t, e, "new.mp3"); tr.Title != "New" {
		t.Fatalf("%+v", tr)
	}

	// a changed file (size differs): only it is re-read and picked up
	writeFile(t, filepath.Join(e.music, "plain.mp3"), mp3Bytes(id3{title: "Retagged", artist: "Solo", album: "X"}))
	e.trigger(t)
	if n := cr.reset(); n != 1 {
		t.Fatalf("after changing a file read %d, want 1", n)
	}
	if tr := trackByTitleOrPath(t, e, "plain.mp3"); tr.Title != "Retagged" || tr.Album != "X" {
		t.Fatalf("%+v", tr)
	}

	// switching tag_mode regenerates songs but re-reads nothing
	m := e.sync.MediaConfig()
	m.TagMode = TagFilenameFirst
	e.sync.SetMedia(m)
	if st := e.trigger(t); st.Result != ResultOK {
		t.Fatalf("%+v", st)
	}
	if n := cr.reset(); n != 0 {
		t.Fatalf("tag_mode switch re-read %d files", n)
	}
	if tr := trackByTitleOrPath(t, e, "Name A - Song A.mp3"); tr.Title != "Song A" {
		t.Fatalf("%+v", tr)
	}

	// filename_only: no reads at all and no tag fields
	m.TagMode = TagFilenameOnly
	e.sync.SetMedia(m)
	e.trigger(t)
	if n := cr.reset(); n != 0 {
		t.Fatalf("filename_only read %d files", n)
	}
}
