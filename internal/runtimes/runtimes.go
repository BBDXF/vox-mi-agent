// Package runtimes manages optional language runtimes (bun, node, uv) that the
// user installs on demand from the web UI into <data>/runtime/<name>/, and
// builds the environment handed to child processes (MCP stdio servers,
// run_shell).
package runtimes

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultNodeBase   = "https://unofficial-builds.nodejs.org/download/release"
	DefaultGithubBase = "https://github.com"
)

// ErrBusy is returned when the runtime is being installed.
var ErrBusy = errors.New("runtime is being installed")

// ErrUnknown is returned for names outside the catalog.
var ErrUnknown = errors.New("unknown runtime")

// builtins are shipped in the image; shown read-only.
var builtins = []string{"bash", "curl", "jq", "yq", "fzq"}

// optional lists the installable runtimes in display order.
var optional = []string{"bun", "node", "uv"}

// Status is the API form of one runtime.
type Status struct {
	Name       string `json:"name"`
	Builtin    bool   `json:"builtin"`
	Installed  bool   `json:"installed"`
	Version    string `json:"version"`
	Installing bool   `json:"installing"`
	Error      string `json:"error"`
}

type job struct{}

// Manager owns <root> (= <data>/runtime).
type Manager struct {
	Root       string // e.g. /data/runtime
	NodeBase   string
	GithubBase string
	Client     *http.Client
	GOARCH     string // default runtime.GOARCH
	// Builtin resolves built-in binaries (default exec.LookPath).
	Builtin func(name string) string

	mu   sync.Mutex
	jobs map[string]*job
	errs map[string]string
}

// New returns a Manager rooted at dataDir/runtime.
func New(dataDir string) *Manager {
	return &Manager{
		Root: filepath.Join(dataDir, "runtime"), NodeBase: DefaultNodeBase, GithubBase: DefaultGithubBase,
		Client: &http.Client{}, GOARCH: runtime.GOARCH, Builtin: func(n string) string { p, _ := exec.LookPath(n); return p },
		jobs: map[string]*job{}, errs: map[string]string{},
	}
}

func (m *Manager) dir(name string) string { return filepath.Join(m.Root, name) }

// binDir is the directory added to PATH for an installed runtime.
func (m *Manager) binDir(name string) string { return filepath.Join(m.dir(name), "bin") }

// Workspace is the default working directory for run_shell.
func (m *Manager) Workspace() string { return filepath.Join(filepath.Dir(m.Root), "workspace") }

// ---------------------------------------------------------------- catalog

// Asset describes what to download for a runtime.
type Asset struct {
	URL     string // archive
	Name    string // archive file name (key in checksum file)
	SumURL  string // checksum file ("" = none)
	SumKind string // "list" (SHASUMS256.txt) or "single" (<file>.sha256)
	Zip     bool
	Version string // known version (node only)
	Main    string // binary relative to bin/ used for --version
}

func (m *Manager) arch(node bool) (string, error) {
	switch m.GOARCH {
	case "amd64":
		if node {
			return "x64", nil
		}
		return "x64", nil
	case "arm64":
		if node {
			return "arm64", nil
		}
		return "aarch64", nil
	}
	return "", fmt.Errorf("unsupported architecture %q", m.GOARCH)
}

// Asset resolves the download for name (node needs network for index.json).
func (m *Manager) Asset(ctx context.Context, name string) (*Asset, error) {
	switch name {
	case "bun":
		a, err := m.arch(false)
		if err != nil {
			return nil, err
		}
		file := "bun-linux-" + a + "-musl.zip"
		base := m.GithubBase + "/oven-sh/bun/releases/latest/download/"
		return &Asset{URL: base + file, Name: file, SumURL: base + "SHASUMS256.txt", SumKind: "list", Zip: true, Main: "bun"}, nil
	case "uv":
		a := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[m.GOARCH]
		if a == "" {
			return nil, fmt.Errorf("unsupported architecture %q", m.GOARCH)
		}
		file := "uv-" + a + "-unknown-linux-musl.tar.gz"
		u := m.GithubBase + "/astral-sh/uv/releases/latest/download/" + file
		return &Asset{URL: u, Name: file, SumURL: u + ".sha256", SumKind: "single", Main: "uv"}, nil
	case "node":
		a, err := m.arch(true)
		if err != nil {
			return nil, err
		}
		plat := "linux-" + a + "-musl"
		var idx []struct {
			Version string          `json:"version"`
			LTS     json.RawMessage `json:"lts"`
			Files   []string        `json:"files"`
		}
		if err := m.getJSON(ctx, m.NodeBase+"/index.json", &idx); err != nil {
			return nil, fmt.Errorf("node index: %w", err)
		}
		for _, e := range idx { // newest first
			if s := string(e.LTS); s == "" || s == "false" || s == "null" {
				continue
			}
			for _, f := range e.Files {
				if f == plat {
					file := "node-" + e.Version + "-" + plat + ".tar.gz"
					base := m.NodeBase + "/" + e.Version + "/"
					return &Asset{URL: base + file, Name: file, SumURL: base + "SHASUMS256.txt", SumKind: "list",
						Version: strings.TrimPrefix(e.Version, "v"), Main: "node"}, nil
				}
			}
		}
		return nil, fmt.Errorf("no LTS node build for %s", plat)
	}
	return nil, ErrUnknown
}

func (m *Manager) getJSON(ctx context.Context, url string, v any) error {
	resp, err := m.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(v)
}

func (m *Manager) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// ---------------------------------------------------------------- status

func known(name string) bool {
	for _, n := range optional {
		if n == name {
			return true
		}
	}
	return false
}

// List returns builtins then optional runtimes.
func (m *Manager) List() []Status {
	out := []Status{}
	for _, n := range builtins {
		ok := m.Builtin != nil && m.Builtin(n) != ""
		out = append(out, Status{Name: n, Builtin: true, Installed: ok})
	}
	for _, n := range optional {
		out = append(out, m.Get(n))
	}
	return out
}

// Get returns the status of one optional runtime.
func (m *Manager) Get(name string) Status {
	st := Status{Name: name}
	m.mu.Lock()
	j := m.jobs[name]
	st.Error = m.errs[name]
	m.mu.Unlock()
	if j != nil {
		st.Installing = true
	}
	bin := filepath.Join(m.binDir(name), name)
	if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
		st.Installed = true
		st.Version = m.version(name)
	}
	return st
}

func (m *Manager) version(name string) string {
	if b, err := os.ReadFile(filepath.Join(m.dir(name), ".version")); err == nil {
		return strings.TrimSpace(string(b))
	}
	v, _ := probeVersion(m.binDir(name), name)
	return v
}

func probeVersion(binDir, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, name), "--version")
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return "", fmt.Errorf("%s --version: empty output", name)
	}
	if f[0] == name && len(f) > 1 {
		f = f[1:]
	}
	return strings.TrimPrefix(f[0], "v"), nil
}

// ---------------------------------------------------------------- install

// Install starts an asynchronous install. Calling it while the runtime is
// already installing is a no-op. Done is closed when the job ends (nil when
// no new job was started).
func (m *Manager) Install(name string) (done <-chan struct{}, err error) {
	if !known(name) {
		return nil, ErrUnknown
	}
	m.mu.Lock()
	if m.jobs[name] != nil {
		m.mu.Unlock()
		return nil, nil
	}
	j := &job{}
	m.jobs[name] = j
	delete(m.errs, name)
	m.mu.Unlock()
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		err := m.install(context.Background(), name, j)
		m.mu.Lock()
		delete(m.jobs, name)
		if err != nil {
			msg := err.Error()
			if len(msg) > 500 {
				msg = msg[:500] + "…"
			}
			m.errs[name] = msg
			slog.Error("runtime install failed", "runtime", name, "err", msg)
		}
		m.mu.Unlock()
	}()
	return ch, nil
}

// Uninstall removes an installed runtime.
func (m *Manager) Uninstall(name string) error {
	if !known(name) {
		return ErrUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jobs[name] != nil {
		return ErrBusy
	}
	delete(m.errs, name)
	return os.RemoveAll(m.dir(name))
}

func (m *Manager) install(ctx context.Context, name string, j *job) (err error) {
	asset, err := m.Asset(ctx, name)
	if err != nil {
		return err
	}
	tmp := filepath.Join(m.Root, ".tmp-"+name)
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, asset.Name)
	sum, err := m.download(ctx, asset.URL, archive, j)
	if err != nil {
		return err
	}
	if asset.SumURL != "" {
		want, err := m.fetchSum(ctx, asset)
		if err != nil {
			return fmt.Errorf("checksum: %w", err)
		}
		if want != sum {
			return fmt.Errorf("checksum mismatch for %s: got %s want %s", asset.Name, sum, want)
		}
	}
	stage := filepath.Join(tmp, "stage")
	prefix := ""
	if name != "node" { // single-binary archives are placed under bin/
		prefix = "bin"
	}
	if asset.Zip {
		err = extractZip(archive, stage, prefix)
	} else {
		err = extractTarGz(archive, stage, prefix)
	}
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	ver, err := probeVersion(filepath.Join(stage, "bin"), asset.Main)
	if err == nil && asset.Main == "bun" {
		// The release zip ships only `bun`; the official installer adds bunx.
		_ = os.Symlink("bun", filepath.Join(stage, "bin", "bunx"))
	}
	if err != nil {
		return err
	}
	os.WriteFile(filepath.Join(stage, ".version"), []byte(ver+"\n"), 0o644)
	dst := m.dir(name)
	m.mu.Lock() // serialise with Uninstall
	defer m.mu.Unlock()
	os.RemoveAll(dst)
	return os.Rename(stage, dst)
}

func (m *Manager) download(ctx context.Context, url, dst string, j *job) (string, error) {
	resp, err := m.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) fetchSum(ctx context.Context, a *Asset) (string, error) {
	resp, err := m.get(ctx, a.SumURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return parseSum(string(b), a.Name, a.SumKind)
}

func parseSum(text, file, kind string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if kind == "single" && len(f) == 1 {
			return strings.ToLower(f[0]), nil
		}
		if len(f) >= 2 {
			n := strings.TrimPrefix(strings.TrimPrefix(f[1], "*"), "./")
			if n == file || kind == "single" {
				return strings.ToLower(f[0]), nil
			}
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", file)
}

// strip removes the first path component and applies prefix; "" = skip.
func strip(name, prefix string) string {
	name = filepath.ToSlash(filepath.Clean(name))
	i := strings.Index(name, "/")
	if i < 0 {
		return ""
	}
	rest := name[i+1:]
	if rest == "" || rest == "." || strings.HasPrefix(rest, "../") || strings.HasPrefix(rest, "/") {
		return ""
	}
	return filepath.Join(prefix, rest)
}

func extractTarGz(archive, dst, prefix string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := strip(h.Name, prefix)
		if rel == "" {
			continue
		}
		p := filepath.Join(dst, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			err = writeFile(p, tr, os.FileMode(h.Mode)&0o777|0o644)
		case tar.TypeSymlink:
			// keep relative links inside the tree (npm/npx -> ../lib/...)
			target := h.Linkname
			if filepath.IsAbs(target) || !within(dst, filepath.Join(filepath.Dir(p), target)) {
				continue
			}
			if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
				err = os.Symlink(target, p)
			}
		}
		if err != nil {
			return err
		}
	}
}

func within(root, p string) bool {
	r, err := filepath.Rel(root, p)
	return err == nil && r != ".." && !strings.HasPrefix(r, "../")
}

func extractZip(archive, dst, prefix string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		rel := strip(zf.Name, prefix)
		if rel == "" || zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(dst, rel), rc, zf.Mode()&0o777|0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---------------------------------------------------------------- env

// ChildEnv returns the environment for child processes: os.Environ with PATH
// prefixed by every installed runtime's bin dir plus cache/home variables.
func (m *Manager) ChildEnv() []string {
	var dirs []string
	if ents, err := os.ReadDir(m.Root); err == nil {
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "cache" && e.Name() != "home" {
				dirs = append(dirs, filepath.Join(m.Root, e.Name(), "bin"))
			}
		}
	}
	sort.Strings(dirs)
	path := os.Getenv("PATH")
	if len(dirs) > 0 {
		path = strings.Join(dirs, ":") + ":" + path
	}
	cache := filepath.Join(m.Root, "cache")
	set := map[string]string{
		"PATH":                  path,
		"NPM_CONFIG_CACHE":      filepath.Join(cache, "npm"),
		"BUN_INSTALL_CACHE_DIR": filepath.Join(cache, "bun"),
		"UV_CACHE_DIR":          filepath.Join(cache, "uv"),
		"UV_TOOL_DIR":           filepath.Join(cache, "uv-tools"),
		"UV_PYTHON_INSTALL_DIR": filepath.Join(cache, "uv-python"),
	}
	if !writableDir(os.Getenv("HOME")) {
		set["HOME"] = filepath.Join(m.Root, "home")
		os.MkdirAll(set["HOME"], 0o755)
	}
	env := make([]string, 0, len(os.Environ())+len(set))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok {
			env = append(env, kv)
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+set[k])
	}
	return env
}

func writableDir(d string) bool {
	if d == "" {
		return false
	}
	f, err := os.CreateTemp(d, ".w")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
