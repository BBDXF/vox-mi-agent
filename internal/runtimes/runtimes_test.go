package runtimes

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeBin = "#!/bin/sh\necho \"%s 9.8.7\"\n"

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func tgz(t *testing.T, top string, files map[string]string, links map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		tw.WriteHeader(&tar.Header{Name: top + "/" + n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg})
		tw.Write([]byte(c))
	}
	for n, target := range links {
		tw.WriteHeader(&tar.Header{Name: top + "/" + n, Linkname: target, Typeflag: tar.TypeSymlink})
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func fakeServer(t *testing.T, bad bool) (*httptest.Server, *int) {
	uv := tgz(t, "uv-x86_64-unknown-linux-musl", map[string]string{"uv": fmt.Sprintf(fakeBin, "uv"), "uvx": fmt.Sprintf(fakeBin, "uvx")}, nil)
	node := tgz(t, "node-v22.1.0-linux-x64-musl", map[string]string{
		"bin/node": "#!/bin/sh\necho v22.1.0\n", "lib/node_modules/npm/bin/npm-cli.js": "x"},
		map[string]string{"bin/npm": "../lib/node_modules/npm/bin/npm-cli.js"})
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "bun-linux-x64-musl/bun", Method: zip.Deflate})
	w.Write([]byte(fmt.Sprintf(fakeBin, "bun")))
	zw.Close()
	hits := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc("/gh/astral-sh/uv/releases/latest/download/uv-x86_64-unknown-linux-musl.tar.gz", func(w http.ResponseWriter, r *http.Request) { *hits++; w.Write(uv) })
	s := sum(uv)
	if bad {
		s = strings.Repeat("0", 64)
	}
	mux.HandleFunc("/gh/astral-sh/uv/releases/latest/download/uv-x86_64-unknown-linux-musl.tar.gz.sha256", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s *uv-x86_64-unknown-linux-musl.tar.gz\n", s)
	})
	mux.HandleFunc("/gh/oven-sh/bun/releases/latest/download/bun-linux-x64-musl.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(zb.Bytes()) })
	mux.HandleFunc("/gh/oven-sh/bun/releases/latest/download/SHASUMS256.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  other.zip\n%s  bun-linux-x64-musl.zip\n", strings.Repeat("1", 64), sum(zb.Bytes()))
	})
	mux.HandleFunc("/node/index.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"version":"v23.0.0","lts":false,"files":["linux-x64-musl"]},{"version":"v22.1.0","lts":"Jod","files":["linux-arm64-musl","linux-x64-musl"]},{"version":"v20.0.0","lts":"Iron","files":["linux-x64-musl"]}]`)
	})
	mux.HandleFunc("/node/v22.1.0/node-v22.1.0-linux-x64-musl.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(node) })
	mux.HandleFunc("/node/v22.1.0/SHASUMS256.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  node-v22.1.0-linux-x64-musl.tar.gz\n", sum(node))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, hits
}

func newMgr(t *testing.T, srv *httptest.Server, arch string) *Manager {
	m := New(t.TempDir())
	m.NodeBase, m.GithubBase, m.GOARCH = srv.URL+"/node", srv.URL+"/gh", arch
	m.Builtin = func(n string) string {
		if n == "curl" {
			return "/usr/bin/curl"
		}
		return ""
	}
	return m
}

func TestAssetSelection(t *testing.T) {
	srv, _ := fakeServer(t, false)
	for _, c := range []struct{ arch, name, want string }{
		{"amd64", "bun", "/oven-sh/bun/releases/latest/download/bun-linux-x64-musl.zip"},
		{"arm64", "bun", "/oven-sh/bun/releases/latest/download/bun-linux-aarch64-musl.zip"},
		{"amd64", "uv", "/astral-sh/uv/releases/latest/download/uv-x86_64-unknown-linux-musl.tar.gz"},
		{"arm64", "uv", "/astral-sh/uv/releases/latest/download/uv-aarch64-unknown-linux-musl.tar.gz"},
		{"amd64", "node", "/node/v22.1.0/node-v22.1.0-linux-x64-musl.tar.gz"},
		{"arm64", "node", "/node/v22.1.0/node-v22.1.0-linux-arm64-musl.tar.gz"},
	} {
		a, err := newMgr(t, srv, c.arch).Asset(context.Background(), c.name)
		if err != nil || !strings.HasSuffix(a.URL, c.want) {
			t.Errorf("%s/%s: %v %+v", c.arch, c.name, err, a)
		}
	}
	if _, err := newMgr(t, srv, "riscv64").Asset(context.Background(), "uv"); err == nil {
		t.Error("expected unsupported arch")
	}
	if _, err := newMgr(t, srv, "amd64").Asset(context.Background(), "x"); err != ErrUnknown {
		t.Error("expected ErrUnknown")
	}
}

func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Fatal("install timeout")
	}
}

func TestInstallFlow(t *testing.T) {
	srv, hits := fakeServer(t, false)
	m := newMgr(t, srv, "amd64")
	for _, n := range []string{"uv", "bun", "node"} {
		done, err := m.Install(n)
		if err != nil || done == nil {
			t.Fatal(n, err)
		}
		wait(t, done)
		st := m.Get(n)
		if !st.Installed || st.Error != "" || st.Installing || st.Version == "" {
			t.Fatalf("%s: %+v", n, st)
		}
	}
	if v := m.Get("uv").Version; v != "9.8.7" {
		t.Fatalf("version %q", v)
	}
	if v := m.Get("node").Version; v != "22.1.0" {
		t.Fatalf("version %q", v)
	}
	if fi, err := os.Lstat(filepath.Join(m.Root, "node/bin/npm")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("npm symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Root, "uv/bin/uvx")); err != nil {
		t.Fatal(err)
	}
	// status persists across a fresh Manager (dir scan)
	m2 := New(filepath.Dir(m.Root))
	if st := m2.Get("bun"); !st.Installed || st.Version != "9.8.7" {
		t.Fatalf("%+v", st)
	}
	// list shape
	l := m.List()
	if len(l) != 8 || l[0].Name != "bash" || !l[0].Builtin || !l[1].Installed || l[1].Name != "curl" || l[2].Installed {
		t.Fatalf("%+v", l)
	}
	_ = hits
	// uninstall
	if err := m.Uninstall("uv"); err != nil || m.Get("uv").Installed {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Root, ".tmp-uv")); err == nil {
		t.Fatal("tmp left behind")
	}
}

func TestInstallChecksumMismatch(t *testing.T) {
	srv, _ := fakeServer(t, true)
	m := newMgr(t, srv, "amd64")
	done, _ := m.Install("uv")
	wait(t, done)
	st := m.Get("uv")
	if st.Installed || !strings.Contains(st.Error, "checksum mismatch") {
		t.Fatalf("%+v", st)
	}
}

func TestInstallNoopWhileInstalling(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release; http.Error(w, "no", 500) }))
	defer srv.Close()
	m := newMgr(t, srv, "amd64")
	done, _ := m.Install("uv")
	d2, err := m.Install("uv")
	if err != nil || d2 != nil {
		t.Fatal("second install must be a no-op")
	}
	if st := m.Get("uv"); !st.Installing {
		t.Fatalf("%+v", st)
	}
	if err := m.Uninstall("uv"); err != ErrBusy {
		t.Fatal(err)
	}
	close(release)
	wait(t, done)
	if st := m.Get("uv"); st.Installing || st.Error == "" {
		t.Fatalf("%+v", st)
	}
}

func TestChildEnv(t *testing.T) {
	m := New(t.TempDir())
	os.MkdirAll(filepath.Join(m.Root, "node", "bin"), 0o755)
	os.MkdirAll(filepath.Join(m.Root, "uv", "bin"), 0o755)
	os.MkdirAll(filepath.Join(m.Root, "cache"), 0o755)
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/nonexistent")
	env := map[string]string{}
	for _, kv := range m.ChildEnv() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	want := m.Root + "/node/bin:" + m.Root + "/uv/bin:/usr/bin"
	if env["PATH"] != want {
		t.Fatalf("PATH=%q want %q", env["PATH"], want)
	}
	for k, v := range map[string]string{"NPM_CONFIG_CACHE": "npm", "BUN_INSTALL_CACHE_DIR": "bun", "UV_CACHE_DIR": "uv", "UV_TOOL_DIR": "uv-tools", "UV_PYTHON_INSTALL_DIR": "uv-python"} {
		if env[k] != filepath.Join(m.Root, "cache", v) {
			t.Errorf("%s=%q", k, env[k])
		}
	}
	if env["HOME"] != filepath.Join(m.Root, "home") {
		t.Errorf("HOME=%q", env["HOME"])
	}
	t.Setenv("HOME", t.TempDir())
	for _, kv := range m.ChildEnv() {
		if strings.HasPrefix(kv, "HOME=") && kv != "HOME="+os.Getenv("HOME") {
			t.Errorf("writable HOME overridden: %s", kv)
		}
	}
}
