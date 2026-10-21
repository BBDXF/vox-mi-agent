package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFzqSongsRegression ports fzq examples/songs/test.sh: every query of
// cases.json goes through Index.Search (top-1, same strip patterns as
// demo.sh via config defaults); the top hit's file path must contain one of
// the expected substrings, and "expect: []" means no result.
func TestFzqSongsRegression(t *testing.T) {
	toolsDir(t)
	songsTxt, err := os.ReadFile("testdata/songs.txt")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, l := range strings.Split(string(songsTxt), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		files = append(files, l[strings.LastIndex(l, "/")+1:]) // basename; all songs are in one folder
	}
	e := newEnv(t, files, nil)
	st := e.trigger(t)
	if st.Result != ResultOK || st.Tracks != len(files) {
		t.Fatalf("sync: %+v (want %d tracks)", st, len(files))
	}

	var cases []struct {
		Cat    string   `json:"cat"`
		Q      string   `json:"q"`
		Expect []string `json:"expect"`
	}
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}

	pass := 0
	for _, c := range cases {
		hits, _, err := e.index.Search(context.Background(), c.Q, 1, 65)
		if err != nil {
			t.Errorf("[%s] %q: %v", c.Cat, c.Q, err)
			continue
		}
		top := ""
		if len(hits) > 0 {
			tr, ok := e.lib.Lookup(hits[0].ID)
			if !ok {
				t.Errorf("[%s] %q: hit id %s not in library", c.Cat, c.Q, hits[0].ID)
				continue
			}
			top, _ = filepath.Rel(e.music, tr.Path)
		}
		ok := false
		if len(c.Expect) == 0 {
			ok = top == ""
		} else {
			for _, x := range c.Expect {
				if strings.Contains(top, x) {
					ok = true
				}
			}
		}
		if ok {
			pass++
		} else {
			t.Errorf("[%s] %q: top=%q want one of %v", c.Cat, c.Q, top, c.Expect)
		}
	}
	t.Logf("regression: %d/%d passed", pass, len(cases))
	if len(cases) != 79 {
		t.Errorf("expected 79 cases, got %d", len(cases))
	}
}
