// Package mcp connects to external MCP servers (official Go SDK) and exposes
// their tools to the agent through the tools registry as
// mcp__<server>__<tool>.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

const (
	// Prefix of every registered MCP tool name.
	Prefix = "mcp__"
	// DefaultTimeout bounds connecting and each tool call.
	DefaultTimeout = 60 * time.Second
	// MaxResultBytes caps the text returned to the LLM per call.
	MaxResultBytes = 64 << 10
	maxToolName    = 64
)

// ServerConfig describes one MCP server.
type ServerConfig struct {
	Name       string
	Transport  string // http|sse|stdio
	URL        string
	Headers    map[string]string
	Command    string
	Args       []string
	Env        map[string]string
	Timeout    time.Duration
	ToolsAllow []string // original tool names; empty = all
}

// ToolInfo describes a tool offered by a server.
type ToolInfo struct {
	Name        string `json:"name"`        // name registered with the agent
	Original    string `json:"original"`    // name on the server
	Description string `json:"description"` // as reported by the server
}

// ServerStatus is the live state of one configured server.
type ServerStatus struct {
	Name      string     `json:"name"`
	Enabled   bool       `json:"enabled"`
	Transport string     `json:"transport"`
	Connected bool       `json:"connected"`
	Error     string     `json:"error"`
	Tools     []ToolInfo `json:"tools"`
	Retrying  bool       `json:"retrying"`
	NextRetry string     `json:"next_retry,omitempty"` // RFC3339 while Retrying
}

var unsafeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ToolName builds the agent-facing tool name (sanitised, at most 64 chars).
func ToolName(server, tool string) string {
	n := Prefix + unsafeRe.ReplaceAllString(server, "_") + "__" + unsafeRe.ReplaceAllString(tool, "_")
	if len(n) > maxToolName {
		n = n[:maxToolName]
	}
	return n
}

// IsMCPTool reports whether name was registered by this package.
func IsMCPTool(name string) bool { return strings.HasPrefix(name, Prefix) }

type headerRT struct {
	h    map[string]string
	next http.RoundTripper
}

func (t headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		r.Header.Set(k, v)
	}
	return t.next.RoundTrip(r)
}

func transportOf(c ServerConfig) (sdk.Transport, error) {
	hc := &http.Client{}
	if len(c.Headers) > 0 {
		hc.Transport = headerRT{h: c.Headers, next: http.DefaultTransport}
	}
	switch c.Transport {
	case "http":
		return &sdk.StreamableClientTransport{Endpoint: c.URL, HTTPClient: hc}, nil
	case "sse":
		return &sdk.SSEClientTransport{Endpoint: c.URL, HTTPClient: hc}, nil
	case "stdio":
		env := ChildEnv()
		cmd := exec.Command(lookPath(c.Command, env), c.Args...)
		cmd.Env = env
		keys := make([]string, 0, len(c.Env))
		for k := range c.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cmd.Env = append(cmd.Env, k+"="+c.Env[k])
		}
		return &sdk.CommandTransport{Command: cmd}, nil
	}
	return nil, fmt.Errorf("unknown transport %q", c.Transport)
}

// ChildEnv supplies the environment of stdio servers (set by the app to
// runtimes.Manager.ChildEnv so installed runtimes are on PATH).
var ChildEnv = os.Environ

func (c ServerConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// connectTimeout bounds the initial connect + tools/list. stdio servers get
// at least 120s (first uvx/npx run downloads dependencies); others at least 15s.
func (c ServerConfig) connectTimeout() time.Duration {
	min := DefaultTimeout
	if c.Transport == "stdio" {
		min = 120 * time.Second
	}
	if t := c.timeout(); t > min {
		return t
	}
	return min
}

// connect opens a session on tr and lists the server's tools (all pages).
func connect(ctx context.Context, c ServerConfig, tr sdk.Transport) (*sdk.ClientSession, []*sdk.Tool, error) {
	cctx, cancel := context.WithTimeout(ctx, c.connectTimeout())
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "vox-mi-agent", Version: "1"}, nil)
	cs, err := client.Connect(cctx, tr, nil)
	if err != nil {
		return nil, nil, err
	}
	var list []*sdk.Tool
	for t, err := range cs.Tools(cctx, nil) {
		if err != nil {
			cs.Close()
			return nil, nil, fmt.Errorf("list tools: %w", err)
		}
		list = append(list, t)
	}
	return cs, list, nil
}

func allowed(c ServerConfig, name string) bool {
	if len(c.ToolsAllow) == 0 {
		return true
	}
	for _, a := range c.ToolsAllow {
		if a == name {
			return true
		}
	}
	return false
}

// TestResult is the outcome of Test.
type TestResult struct {
	OK        bool       `json:"ok"`
	Error     string     `json:"error,omitempty"`
	LatencyMS int64      `json:"latency_ms"`
	Tools     []ToolInfo `json:"tools"`
}

// Test connects to one (possibly unsaved) server, lists its tools and
// disconnects. Nothing is registered.
func Test(ctx context.Context, c ServerConfig) TestResult {
	start := time.Now()
	res := TestResult{Tools: []ToolInfo{}}
	tr, err := transportOf(c)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	cs, list, err := connect(ctx, c, tr)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = trunc(err.Error(), 300)
		return res
	}
	defer cs.Close()
	res.OK = true
	for _, t := range list {
		res.Tools = append(res.Tools, ToolInfo{Name: ToolName(c.Name, t.Name), Original: t.Name, Description: t.Description})
	}
	return res
}

func trunc(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// Manager owns the MCP connections and the tools they registered.
type Manager struct {
	reg *tools.Registry
	log *slog.Logger

	reloadMu sync.Mutex // serialises Reload / Close
	mu       sync.RWMutex
	sessions []*sdk.ClientSession
	names    []string // registered tool names
	status   []ServerStatus
	gen      uint64
	cancel   context.CancelFunc
	srvNames map[int][]string // registered tool names per server index

	// Backoff returns the wait before retry attempt n (0-based); injectable for tests.
	Backoff func(n int) time.Duration
	dial    func(ctx context.Context, c ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error)
}

var defaultBackoff = []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}

func backoffN(n int) time.Duration {
	if n >= len(defaultBackoff) {
		return defaultBackoff[len(defaultBackoff)-1]
	}
	return defaultBackoff[n]
}

func defaultDial(ctx context.Context, c ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
	tr, err := transportOf(c)
	if err != nil {
		return nil, nil, err
	}
	return connect(ctx, c, tr)
}

// NewManager creates a Manager registering tools into reg.
func NewManager(reg *tools.Registry, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{reg: reg, log: log, status: []ServerStatus{}, Backoff: backoffN, dial: defaultDial}
}

// Status returns a snapshot of the per-server state (configuration order).
func (m *Manager) Status() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerStatus, len(m.status))
	copy(out, m.status)
	return out
}

// Close disconnects everything and unregisters the MCP tools.
func (m *Manager) Close() {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.teardown()
}

func (m *Manager) teardown() {
	m.mu.Lock()
	m.gen++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	sessions, names := m.sessions, m.names
	m.sessions, m.names, m.srvNames = nil, nil, nil
	m.mu.Unlock()
	for _, n := range names {
		m.reg.Unregister(n)
	}
	for _, s := range sessions {
		s.Close()
	}
}

// Server pairs a server config with its enabled flag.
type Server struct {
	ServerConfig
	Enabled bool
}

// Reload replaces every connection with the given configuration. Failures
// are logged and reported in Status; Reload itself never fails.
func (m *Manager) Reload(ctx context.Context, servers []Server) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.teardown()

	st := make([]ServerStatus, len(servers))
	for i, s := range servers {
		st[i] = ServerStatus{Name: s.Name, Enabled: s.Enabled, Transport: s.Transport, Tools: []ToolInfo{}}
	}
	rctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.status = st
	m.srvNames = map[int][]string{}
	gen := m.gen
	m.mu.Unlock()

	type result struct {
		cs   *sdk.ClientSession
		list []*sdk.Tool
		err  error
	}
	res := make([]result, len(servers))
	var wg sync.WaitGroup
	for i, s := range servers {
		if !s.Enabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i].cs, res[i].list, res[i].err = m.dial(ctx, s.ServerConfig)
		}()
	}
	wg.Wait()

	for i, s := range servers {
		if !s.Enabled {
			continue
		}
		r := res[i]
		if r.err != nil {
			m.mu.Lock()
			if m.gen == gen {
				m.status[i].Error = trunc(r.err.Error(), 300)
			}
			m.mu.Unlock()
			m.log.Warn("mcp server unavailable", "server", s.Name, "err", r.err)
			go m.retryLoop(rctx, gen, i, s, 0)
			continue
		}
		if !m.attach(rctx, gen, i, s, r.cs, r.list) {
			r.cs.Close()
		}
	}
}

// attach registers a connected server's tools and status. It returns false
// (and registers nothing) if the generation is stale.
func (m *Manager) attach(rctx context.Context, gen uint64, i int, s Server, cs *sdk.ClientSession, list []*sdk.Tool) bool {
	m.mu.Lock()
	if m.gen != gen {
		m.mu.Unlock()
		return false
	}
	st := &m.status[i]
	st.Connected, st.Error, st.Retrying, st.NextRetry = true, "", false, ""
	st.Tools = []ToolInfo{}
	m.sessions = append(m.sessions, cs)
	var mine []string
	for _, t := range list {
		if !allowed(s.ServerConfig, t.Name) {
			continue
		}
		name := ToolName(s.Name, t.Name)
		if _, exists := m.reg.Get(name); exists {
			m.log.Warn("mcp tool name collision, skipped", "server", s.Name, "tool", t.Name, "name", name)
			continue
		}
		m.reg.Register(&mcpTool{name: name, orig: t.Name, desc: t.Description, schema: schemaOf(t.InputSchema),
			cs: cs, timeout: s.timeout()})
		m.names = append(m.names, name)
		mine = append(mine, name)
		st.Tools = append(st.Tools, ToolInfo{Name: name, Original: t.Name, Description: t.Description})
	}
	m.srvNames[i] = mine
	n := len(st.Tools)
	m.mu.Unlock()
	m.log.Info("mcp server connected", "server", s.Name, "tools", n)
	go func() {
		_ = cs.Wait()
		m.dropped(rctx, gen, i, s, cs)
	}()
	return true
}

// dropped handles a session that ended (e.g. stdio process exited).
func (m *Manager) dropped(rctx context.Context, gen uint64, i int, s Server, cs *sdk.ClientSession) {
	m.mu.Lock()
	if m.gen != gen {
		m.mu.Unlock()
		return
	}
	gone := map[string]bool{}
	for _, n := range m.srvNames[i] {
		m.reg.Unregister(n)
		gone[n] = true
	}
	delete(m.srvNames, i)
	m.names = filterStr(m.names, gone)
	for j, x := range m.sessions {
		if x == cs {
			m.sessions = append(m.sessions[:j:j], m.sessions[j+1:]...)
			break
		}
	}
	m.status[i].Connected = false
	m.status[i].Tools = []ToolInfo{}
	m.status[i].Error = "connection lost"
	m.mu.Unlock()
	m.log.Warn("mcp server disconnected", "server", s.Name)
	m.retryLoop(rctx, gen, i, s, 0)
}

func filterStr(in []string, drop map[string]bool) []string {
	var out []string
	for _, n := range in {
		if !drop[n] {
			out = append(out, n)
		}
	}
	return out
}

// retryLoop reconnects a failed server with backoff until it connects or the
// context is cancelled (next Reload/Close).
func (m *Manager) retryLoop(ctx context.Context, gen uint64, i int, s Server, attempt int) {
	for ; ; attempt++ {
		d := m.Backoff(attempt)
		m.mu.Lock()
		if m.gen != gen {
			m.mu.Unlock()
			return
		}
		m.status[i].Retrying = true
		m.status[i].NextRetry = time.Now().Add(d).Format(time.RFC3339)
		m.mu.Unlock()
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		cs, list, err := m.dial(ctx, s.ServerConfig)
		if err != nil {
			m.mu.Lock()
			if m.gen == gen {
				m.status[i].Error = trunc(err.Error(), 300)
			}
			m.mu.Unlock()
			m.log.Warn("mcp server retry failed", "server", s.Name, "attempt", attempt+1, "err", err)
			continue
		}
		if !m.attach(ctx, gen, i, s, cs, list) {
			cs.Close()
		}
		return
	}
}

func schemaOf(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b
}

type mcpTool struct {
	name, orig, desc string
	schema           json.RawMessage
	cs               *sdk.ClientSession
	timeout          time.Duration
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.desc }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }

func (t *mcpTool) Call(ctx context.Context, args json.RawMessage) (any, error) {
	var a any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	res, err := t.cs.CallTool(ctx, &sdk.CallToolParams{Name: t.orig, Arguments: a})
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(tc.Text)
		}
	}
	text := sb.String()
	if text == "" && res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			text = string(b)
		}
	}
	if len(text) > MaxResultBytes {
		text = strings.ToValidUTF8(text[:MaxResultBytes], "") + "…(truncated)"
	}
	if res.IsError {
		if text == "" {
			text = "tool reported an error"
		}
		return nil, errors.New(text)
	}
	return text, nil
}

// lookPath resolves a bare command name against the child's PATH (which
// includes installed runtimes), not the server process's own PATH.
func lookPath(name string, env []string) string {
	if strings.ContainsRune(name, '/') {
		return name
	}
	for i := len(env) - 1; i >= 0; i-- {
		if p, ok := strings.CutPrefix(env[i], "PATH="); ok {
			for _, dir := range filepath.SplitList(p) {
				f := filepath.Join(dir, name)
				if st, err := os.Stat(f); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
					return f
				}
			}
			break
		}
	}
	return name
}
