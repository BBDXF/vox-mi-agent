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
	DefaultTimeout = 15 * time.Second
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
		cmd := exec.Command(c.Command, c.Args...)
		cmd.Env = os.Environ()
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

func (c ServerConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// connect opens a session on tr and lists the server's tools (all pages).
func connect(ctx context.Context, c ServerConfig, tr sdk.Transport) (*sdk.ClientSession, []*sdk.Tool, error) {
	cctx, cancel := context.WithTimeout(ctx, c.timeout())
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
}

// NewManager creates a Manager registering tools into reg.
func NewManager(reg *tools.Registry, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{reg: reg, log: log, status: []ServerStatus{}}
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
	sessions, names := m.sessions, m.names
	m.sessions, m.names = nil, nil
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
			tr, err := transportOf(s.ServerConfig)
			if err != nil {
				res[i].err = err
				return
			}
			res[i].cs, res[i].list, res[i].err = connect(ctx, s.ServerConfig, tr)
		}()
	}
	wg.Wait()

	var sessions []*sdk.ClientSession
	var names []string
	for i, s := range servers {
		if !s.Enabled {
			continue
		}
		r := res[i]
		if r.err != nil {
			st[i].Error = trunc(r.err.Error(), 300)
			m.log.Warn("mcp server unavailable", "server", s.Name, "err", r.err)
			continue
		}
		st[i].Connected = true
		sessions = append(sessions, r.cs)
		for _, t := range r.list {
			if !allowed(s.ServerConfig, t.Name) {
				continue
			}
			name := ToolName(s.Name, t.Name)
			if _, exists := m.reg.Get(name); exists {
				m.log.Warn("mcp tool name collision, skipped", "server", s.Name, "tool", t.Name, "name", name)
				continue
			}
			m.reg.Register(&mcpTool{name: name, orig: t.Name, desc: t.Description, schema: schemaOf(t.InputSchema),
				cs: r.cs, timeout: s.timeout()})
			names = append(names, name)
			st[i].Tools = append(st[i].Tools, ToolInfo{Name: name, Original: t.Name, Description: t.Description})
		}
		m.log.Info("mcp server connected", "server", s.Name, "tools", len(st[i].Tools))
	}
	m.mu.Lock()
	m.sessions, m.names, m.status = sessions, names, st
	m.mu.Unlock()
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
