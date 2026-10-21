package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/llm"
)

func mkdirs(t *testing.T, base string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(base, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

type dirsResp struct {
	Path      string  `json:"path"`
	Parent    *string `json:"parent"`
	Truncated bool    `json:"truncated"`
	Entries   []struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Symlink bool   `json:"symlink"`
	} `json:"entries"`
}

func TestFSDirs(t *testing.T) {
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")
	e := newEnv(t, llm.NewFake(), nil)
	root := t.TempDir()
	mkdirs(t, root, "b", "A", ".hidden", "c/inner")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "b"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "file.txt"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}

	resp, b := e.do(t, "GET", "/api/fs/dirs?path="+root, "")
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	d := decode[dirsResp](t, b)
	var names []string
	for _, en := range d.Entries {
		names = append(names, en.Name)
		if en.Symlink != (en.Name == "link") {
			t.Errorf("symlink flag on %s", en.Name)
		}
		if en.Path != filepath.Join(root, en.Name) {
			t.Errorf("path %s", en.Path)
		}
	}
	if got := strings.Join(names, ","); got != "A,b,c,link" {
		t.Fatalf("entries %q (want sorted, dirs only, no hidden, no file symlinks)", got)
	}
	if d.Parent == nil || *d.Parent != filepath.Dir(root) {
		t.Fatalf("parent %v", d.Parent)
	}

	// default path "/" (no roots): parent null
	_, b = e.do(t, "GET", "/api/fs/dirs", "")
	if d := decode[dirsResp](t, b); d.Path != "/" || d.Parent != nil {
		t.Fatalf("default: %+v", d)
	}
	if !strings.Contains(string(b), `"parent":null`) {
		t.Fatalf("parent should serialize as null: %s", b)
	}

	// errors
	for _, tc := range []struct {
		q    string
		code int
	}{
		{"?path=relative", 400},
		{"?path=" + filepath.Join(root, "file.txt"), 400},
		{"?path=" + filepath.Join(root, "nope"), 404},
	} {
		resp, b := e.do(t, "GET", "/api/fs/dirs"+tc.q, "")
		if resp.StatusCode != tc.code || !strings.Contains(string(b), `"error"`) {
			t.Errorf("%s: %d %s", tc.q, resp.StatusCode, b)
		}
	}

	// truncation
	big := t.TempDir()
	for i := 0; i < maxDirEntries+5; i++ {
		mkdirs(t, big, fmt.Sprintf("d%04d", i))
	}
	_, b = e.do(t, "GET", "/api/fs/dirs?path="+big, "")
	if d := decode[dirsResp](t, b); !d.Truncated || len(d.Entries) != maxDirEntries {
		t.Fatalf("truncated=%v n=%d", d.Truncated, len(d.Entries))
	}
}

func TestFSDirsRootRestriction(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mkdirs(t, root, "music/rock", ".secret")
	mkdirs(t, other, "x")
	if err := os.Symlink(other, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", root+":/nonexistent")
	e := newEnv(t, llm.NewFake(), nil)

	// default = first root, parent null at root
	resp, b := e.do(t, "GET", "/api/fs/dirs", "")
	d := decode[dirsResp](t, b)
	if resp.StatusCode != 200 || d.Path != root || d.Parent != nil {
		t.Fatalf("root: %d %s", resp.StatusCode, b)
	}
	var names []string
	for _, en := range d.Entries {
		names = append(names, en.Name)
	}
	if strings.Join(names, ",") != "music" { // escape symlink hidden, .secret skipped
		t.Fatalf("entries %v", names)
	}
	// below root: parent allowed
	_, b = e.do(t, "GET", "/api/fs/dirs?path="+root+"/music/rock", "")
	if d := decode[dirsResp](t, b); d.Parent == nil || *d.Parent != root+"/music" {
		t.Fatalf("%s", b)
	}
	// outside root / symlink escape / traversal => 403
	for _, p := range []string{other, "/", filepath.Dir(root), filepath.Join(root, "escape"), root + "/../" + filepath.Base(other)} {
		resp, b := e.do(t, "GET", "/api/fs/dirs?path="+p, "")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d %s", p, resp.StatusCode, b)
		}
	}
}

func TestFSValidate(t *testing.T) {
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")
	e := newEnv(t, llm.NewFake(), nil)
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("s%d.mp3", i)), nil, 0o644)
	}
	_ = os.WriteFile(filepath.Join(dir, "x.txt"), nil, 0o644)
	mkdirs(t, dir, "sub")
	_ = os.WriteFile(filepath.Join(dir, "sub", "u.FLAC"), nil, 0o644)

	type vr struct {
		OK       bool   `json:"ok"`
		Reason   string `json:"reason"`
		Estimate int    `json:"audio_files_estimate"`
		Capped   bool   `json:"audio_files_capped"`
		Label    string `json:"audio_files_label"`
	}
	post := func(p string) vr {
		body, _ := json.Marshal(map[string]string{"path": p})
		resp, b := e.do(t, "POST", "/api/fs/validate", string(body))
		if resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, b)
		}
		return decode[vr](t, b)
	}
	if r := post(dir); !r.OK || r.Estimate != 6 || r.Capped || r.Label != "6" {
		t.Fatalf("%+v", r)
	}
	if r := post(filepath.Join(dir, "x.txt")); r.OK || !strings.Contains(r.Reason, "not a directory") {
		t.Fatalf("%+v", r)
	}
	if r := post(filepath.Join(dir, "nope")); r.OK || !strings.Contains(r.Reason, "does not exist") {
		t.Fatalf("%+v", r)
	}
	if r := post("rel"); r.OK || !strings.Contains(r.Reason, "absolute") {
		t.Fatalf("%+v", r)
	}
	big := t.TempDir()
	for i := 0; i < maxCountFiles+50; i++ {
		_ = os.WriteFile(filepath.Join(big, fmt.Sprintf("%05d.mp3", i)), nil, 0o644)
	}
	if r := post(big); !r.OK || !r.Capped || r.Estimate != maxCountFiles || r.Label != "2000+" {
		t.Fatalf("%+v", r)
	}
	// roots
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "/nonexistent")
	if r := post(dir); r.OK || !strings.Contains(r.Reason, "accessible") {
		t.Fatalf("%+v", r)
	}
}

func TestPutMediaSettings(t *testing.T) {
	t.Setenv("TRIM_DATA_ACCESSIBLE_PATHS", "")
	e := newEnv(t, llm.NewFake(), nil)
	music := t.TempDir()
	for _, n := range []string{"Foo - Bar.mp3", "Baz - Qux.flac"} {
		_ = os.WriteFile(filepath.Join(music, n), nil, 0o644)
	}

	// GET shows extensions + accessible_roots (empty when unset)
	_, b := e.do(t, "GET", "/api/settings", "")
	s := decode[struct {
		Media struct {
			Extensions []string `json:"extensions"`
			Roots      []string `json:"accessible_roots"`
		} `json:"media"`
	}](t, b)
	if len(s.Media.Extensions) == 0 || s.Media.Roots == nil || len(s.Media.Roots) != 0 {
		t.Fatalf("%s", b)
	}
	if !strings.Contains(string(b), `"accessible_roots":[]`) {
		t.Fatalf("%s", b)
	}

	// validation failure
	resp, b := e.do(t, "PUT", "/api/settings/media", `{"folders":["/definitely/missing","rel"]}`)
	er := decode[struct {
		Error  string            `json:"error"`
		Fields map[string]string `json:"fields"`
	}](t, b)
	if resp.StatusCode != 400 || er.Error == "" || er.Fields["/definitely/missing"] == "" || er.Fields["rel"] == "" {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings/media", `{}`); resp.StatusCode != 400 {
		t.Fatalf("missing folders: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings/media", `{nope`); resp.StatusCode != 400 {
		t.Fatalf("bad json: %d", resp.StatusCode)
	}

	// success: applied live, sync runs, library changes
	body, _ := json.Marshal(map[string]any{"folders": []string{music}, "extensions": []string{"mp3"}, "sync_interval": "0"})
	resp, b = e.do(t, "PUT", "/api/settings/media", string(body))
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"sync_started":true`) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	deadline := time.Now().Add(15 * time.Second)
	for e.app.Library.Len() != 1 || e.app.Syncer.Status().Running {
		if time.Now().After(deadline) {
			t.Fatalf("library %d status %+v", e.app.Library.Len(), e.app.Syncer.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, b = e.do(t, "GET", "/api/settings", "")
	s2 := decode[struct {
		Media struct {
			Folders      []string `json:"folders"`
			Extensions   []string `json:"extensions"`
			SyncInterval string   `json:"sync_interval"`
		} `json:"media"`
	}](t, b)
	if len(s2.Media.Folders) != 1 || s2.Media.Folders[0] != music || len(s2.Media.Extensions) != 1 || s2.Media.SyncInterval != "0s" {
		t.Fatalf("%s", b)
	}
}
