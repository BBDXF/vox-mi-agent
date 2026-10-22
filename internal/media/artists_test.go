package media

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

func TestSplitArtists(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"周杰伦", []string{"周杰伦"}},
		{"Alan Walker _ Iselin Solheim", []string{"Alan Walker", "Iselin Solheim"}},
		{"A、B", []string{"A", "B"}},
		{"周杰伦、费玉清、方文山", []string{"周杰伦", "费玉清", "方文山"}},
		{"A feat. B", []string{"A", "B"}},
		{"A Feat B", []string{"A", "B"}},
		{"A FT. B", []string{"A", "B"}},
		{"A ft B", []string{"A", "B"}},
		{"A & B", []string{"A", "B"}},
		{"A&B", []string{"A", "B"}},
		{"A / B", []string{"A", "B"}},
		{"A,B", []string{"A", "B"}},
		{"A, B", []string{"A", "B"}},
		{"A，B", []string{"A", "B"}},
		{"Max x Min", []string{"Max", "Min"}},
		{"Max X Min", []string{"Max", "Min"}},
		{"Alex Maxx", []string{"Alex Maxx"}},       // x inside a word
		{"Xavier", []string{"Xavier"}},             // leading X
		{"AC/DC", []string{"AC/DC"}},               // slash needs spaces
		{"snake_case", []string{"snake_case"}},     // underscore needs spaces
		{"A _ B & A", []string{"A", "B"}},          // duplicates dropped
		{"A feat. B x C", []string{"A", "B", "C"}}, // mixed
		{"  A  _  B  ", []string{"A", "B"}},        // spacing
		{"Lefty Fox", []string{"Lefty Fox"}},       // "ft"/"x" substrings
		{"Daft Punk feat Pharrell", []string{"Daft Punk", "Pharrell"}},
	} {
		if got := SplitArtists(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitArtists(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseNameKeepsJoinedArtist(t *testing.T) {
	re := regexp.MustCompile(config.Defaults().Media.FilenamePattern)
	a, ti := parseName(re, "Alan Walker _ Iselin Solheim - Faded.mp3")
	if a != "Alan Walker _ Iselin Solheim" || ti != "Faded" {
		t.Fatalf("got %q %q", a, ti)
	}
	// a title containing a standalone x is never split; only the artist is
	a, ti = parseName(re, "Max x Min - Mix x Match.mp3")
	if a != "Max x Min" || ti != "Mix x Match" {
		t.Fatalf("got %q %q", a, ti)
	}
	if got := SplitArtists(a); !reflect.DeepEqual(got, []string{"Max", "Min"}) {
		t.Fatalf("artists %q", got)
	}
}

func TestSyncMultiArtist(t *testing.T) {
	e := newEnv(t, []string{
		"Alan Walker _ Iselin Solheim - Faded.mp3",
		"周杰伦、费玉清 - 千里之外.mp3",
		"Taylor feat. Zedd - Clarity.mp3",
		"Max x Min - Mix x Match.mp3",
		"Boxx - X Marks The Spot.mp3",
		"Solo - Song.mp3",
	}, nil)
	if st := e.trigger(t); st.Result != ResultOK || st.Tracks != 6 {
		t.Fatalf("%+v", st)
	}
	raw, err := os.ReadFile(SongsPath(e.cfg.DataDir))
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]Track{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var tr Track
		if err := json.Unmarshal([]byte(l), &tr); err != nil {
			t.Fatal(err)
		}
		byTitle[tr.Title] = tr
	}
	faded := byTitle["Faded"]
	if faded.Artist != "Alan Walker _ Iselin Solheim" || !reflect.DeepEqual(faded.Artists, []string{"Alan Walker", "Iselin Solheim"}) {
		t.Fatalf("faded: %+v", faded)
	}
	if !strings.Contains(string(raw), `"artists":["Alan Walker","Iselin Solheim"]`) {
		t.Fatalf("songs file lacks artists array: %s", raw)
	}
	if got := byTitle["Mix x Match"].Artists; !reflect.DeepEqual(got, []string{"Max", "Min"}) {
		t.Fatalf("Max x Min: %q", got)
	}
	if got := byTitle["X Marks The Spot"].Artists; !reflect.DeepEqual(got, []string{"Boxx"}) {
		t.Fatalf("Boxx: %q", got)
	}
	if got := byTitle["千里之外"].Artists; !reflect.DeepEqual(got, []string{"周杰伦", "费玉清"}) {
		t.Fatalf("cn: %q", got)
	}

	// fzq matches by a secondary artist (against the artists[] key)
	for q, wantTitle := range map[string]string{
		"Iselin Solheim": "Faded",
		"费玉清":            "千里之外",
		"Zedd":           "Clarity",
	} {
		hits, _, err := e.index.Search(context.Background(), q, 3, 65)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].Title != wantTitle {
			t.Errorf("search %q: %+v, want top %q", q, hits, wantTitle)
		}
	}
	// artist + title still works with the joined artist
	hits, _, err := e.index.Search(context.Background(), "Iselin Solheim Faded", 1, 65)
	if err != nil || len(hits) != 1 || hits[0].Title != "Faded" {
		t.Fatalf("artist+title: %+v %v", hits, err)
	}
}

func TestLibraryDerivesArtistsFromOldFile(t *testing.T) {
	e := newEnv(t, nil, nil)
	songs := SongsPath(e.cfg.DataDir)
	if err := os.MkdirAll(strings.TrimSuffix(songs, "songs.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"id":"abc","title":"T","artist":"A _ B","album":"","alias":[],"path":"/m/A _ B - T.mp3"}` + "\n"
	if err := os.WriteFile(songs, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.Reload(); err != nil {
		t.Fatal(err)
	}
	tr, _ := e.lib.Lookup("abc")
	if !reflect.DeepEqual(tr.Artists, []string{"A", "B"}) {
		t.Fatalf("%+v", tr)
	}
}
