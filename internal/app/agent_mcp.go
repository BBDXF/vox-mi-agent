package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/mcp"
)

const maxMCPServers = 20

func agentSettings(c config.Agent) agent.Settings {
	var t *float64
	if c.Temperature != nil {
		v := *c.Temperature
		t = &v
	}
	return agent.Settings{SystemPrompt: c.SystemPrompt, MaxSteps: c.MaxSteps, Temperature: t,
		TurnTimeout: c.TurnTimeout, HistoryMessages: c.HistoryMessages,
		DisabledTools: append([]string(nil), c.ToolsDisabled...)}
}

// builtinTools lists the registered non-MCP tools.
func (a *App) builtinTools() []map[string]string {
	out := []map[string]string{}
	for _, t := range a.Tools.List() {
		if !mcp.IsMCPTool(t.Name()) {
			out = append(out, map[string]string{"name": t.Name(), "description": t.Description()})
		}
	}
	return out
}

func agentSection() *section {
	temp := &fieldDef{
		Field: Field{Key: "temperature", Type: TFloat, Default: nil,
			Description: "采样温度 0–2；留空（null）表示使用 llm.temperature"},
		get: func(c *config.Config) any {
			if c.Agent.Temperature == nil {
				return nil
			}
			return *c.Agent.Temperature
		},
		set: func(c *config.Config, raw json.RawMessage) error {
			if isNull(raw) {
				c.Agent.Temperature = nil
				return nil
			}
			var f float64
			if json.Unmarshal(raw, &f) != nil {
				return fmt.Errorf("must be a number or null")
			}
			c.Agent.Temperature = &f
			return nil
		},
		node: func(c *config.Config) (*yamlNode, error) {
			if c.Agent.Temperature == nil {
				return &yamlNode{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
			}
			return yamlOf(*c.Agent.Temperature)
		},
	}
	s := &section{
		Name: "agent", Title: "智能体",
		Description: "系统提示词、对话循环参数与内置工具开关。修改后立即生效。",
		fields: []*fieldDef{
			withDefault(strField("system_prompt", "系统提示词；留空使用内置默认提示词（见 default_system_prompt）",
				func(c *config.Config) *string { return &c.Agent.SystemPrompt }), ""),
			intField("max_steps", "每轮对话最多调用 LLM 的次数（工具轮数，1–50）", func(c *config.Config) *int { return &c.Agent.MaxSteps }),
			temp,
			durField("turn_timeout", "单轮对话总超时（如 120s；0 表示不限，最大 1h）", func(c *config.Config) *time.Duration { return &c.Agent.TurnTimeout }),
			intField("history_messages", "作为上下文保留的最近消息条数（0 表示全部，最大 1000）", func(c *config.Config) *int { return &c.Agent.HistoryMessages }),
			stringsField("tools_disabled", "禁用的内置工具名称（不会发送给 LLM）；见 available_tools",
				func(c *config.Config) *[]string { return &c.Agent.ToolsDisabled }),
		},
	}
	s.getFn = func(ctx context.Context, a *App) (map[string]any, error) {
		out := s.values(a.Config())
		out["default_system_prompt"] = agent.DefaultSystemPrompt
		out["available_tools"] = a.builtinTools()
		return out, nil
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		g := &n.Agent
		if provided["max_steps"] && (g.MaxSteps < 1 || g.MaxSteps > 50) {
			fe["max_steps"] = "must be between 1 and 50"
		}
		if provided["temperature"] && g.Temperature != nil && (*g.Temperature < 0 || *g.Temperature > 2) {
			fe["temperature"] = "must be between 0 and 2"
		}
		if provided["turn_timeout"] && (g.TurnTimeout < 0 || g.TurnTimeout > time.Hour) {
			fe["turn_timeout"] = "must be between 0 and 1h"
		}
		if provided["history_messages"] && (g.HistoryMessages < 0 || g.HistoryMessages > 1000) {
			fe["history_messages"] = "must be between 0 and 1000"
		}
		if provided["tools_disabled"] {
			known := map[string]bool{}
			for _, t := range a.builtinTools() {
				known[t["name"]] = true
			}
			seen := map[string]bool{}
			clean := []string{}
			for _, name := range g.ToolsDisabled {
				name = strings.TrimSpace(name)
				if !known[name] {
					fe["tools_disabled"] = fmt.Sprintf("unknown built-in tool %q", name)
					break
				}
				if !seen[name] {
					seen[name] = true
					clean = append(clean, name)
				}
			}
			sort.Strings(clean)
			g.ToolsDisabled = clean
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.Agent.Apply(agentSettings(n.Agent))
		return map[string]any{"max_steps": n.Agent.MaxSteps, "tools_disabled": n.Agent.ToolsDisabled}, nil, nil
	}
	return s
}

// ---------------------------------------------------------------- mcp

// MCPServerDTO is the API form of one MCP server (headers/env values are
// masked in GET responses; masked values sent back keep the stored secret).
type MCPServerDTO struct {
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	Transport  string            `json:"transport"`
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	Timeout    string            `json:"timeout"`
	ToolsAllow []string          `json:"tools_allow"`
}

func dtoOf(m config.MCPServer, mask bool) MCPServerDTO {
	h, e := m.Headers, m.Env
	if mask {
		h, e = config.MaskValues(h), config.MaskValues(e)
	}
	if h == nil {
		h = map[string]string{}
	}
	if e == nil {
		e = map[string]string{}
	}
	d := MCPServerDTO{Name: m.Name, Enabled: m.Enabled, Transport: m.Transport, URL: m.URL, Headers: h,
		Command: m.Command, Args: append([]string{}, m.Args...), Env: e, Timeout: FmtDuration(m.Timeout),
		ToolsAllow: append([]string{}, m.ToolsAllow...)}
	return d
}

// keepSecrets replaces values equal to the mask of the stored value (same
// server, same key) by the stored value.
func keepSecrets(sent, stored map[string]string) map[string]string {
	out := make(map[string]string, len(sent))
	for k, v := range sent {
		if old, ok := stored[k]; ok && v == config.MaskSecret(old) {
			v = old
		}
		out[k] = v
	}
	return out
}

func (d MCPServerDTO) toConfig(old []config.MCPServer) (config.MCPServer, error) {
	m := config.MCPServer{Name: strings.TrimSpace(d.Name), Enabled: d.Enabled, Transport: strings.TrimSpace(d.Transport),
		URL: strings.TrimSpace(d.URL), Command: strings.TrimSpace(d.Command), Args: append([]string{}, d.Args...),
		ToolsAllow: []string{}, Headers: d.Headers, Env: d.Env}
	for _, o := range old {
		if o.Name == m.Name {
			m.Headers, m.Env = keepSecrets(d.Headers, o.Headers), keepSecrets(d.Env, o.Env)
		}
	}
	if m.Headers == nil {
		m.Headers = map[string]string{}
	}
	if m.Env == nil {
		m.Env = map[string]string{}
	}
	for _, t := range d.ToolsAllow {
		if t = strings.TrimSpace(t); t != "" {
			m.ToolsAllow = append(m.ToolsAllow, t)
		}
	}
	if ts := strings.TrimSpace(d.Timeout); ts != "" {
		dur, err := time.ParseDuration(ts)
		if err != nil {
			return m, fmt.Errorf("timeout: not a valid duration (e.g. 15s)")
		}
		m.Timeout = dur
	}
	return m, nil
}

func mcpServers(l []config.MCPServer) []mcp.Server {
	out := make([]mcp.Server, 0, len(l))
	for _, m := range l {
		out = append(out, mcp.Server{Enabled: m.Enabled, ServerConfig: mcpConfig(m)})
	}
	return out
}

func mcpConfig(m config.MCPServer) mcp.ServerConfig {
	return mcp.ServerConfig{Name: m.Name, Transport: m.Transport, URL: m.URL, Headers: m.Headers,
		Command: m.Command, Args: m.Args, Env: m.Env, Timeout: m.Timeout, ToolsAllow: m.ToolsAllow}
}

func mcpSection() *section {
	const keysMsg = "must be an array of {name, enabled, transport, url, headers, command, args, env, timeout, tools_allow}"
	itemFields := []Field{
		{Key: "name", Type: TString, Default: "", Description: "服务名（唯一，^[A-Za-z][A-Za-z0-9_-]{0,31}$），工具名为 mcp__<name>__<tool>"},
		{Key: "enabled", Type: TBool, Default: true, Description: "是否启用"},
		{Key: "transport", Type: TEnum, Default: "http", Enum: []string{"http", "sse", "stdio"}, Description: "传输方式：http=Streamable HTTP，sse=旧版 SSE，stdio=本地子进程"},
		{Key: "url", Type: TString, Default: "", Description: "http/sse 的服务地址"},
		{Key: "headers", Type: TMap, Default: map[string]string{}, Secret: true, Description: "http/sse 请求头（敏感，读取时仅显示末 4 位；原样回传表示保持不变）"},
		{Key: "command", Type: TString, Default: "", Description: "stdio 启动命令"},
		{Key: "args", Type: TStrings, Default: []string{}, Description: "stdio 命令参数"},
		{Key: "env", Type: TMap, Default: map[string]string{}, Secret: true, Description: "stdio 额外环境变量（敏感，同 headers）"},
		{Key: "timeout", Type: TDuration, Default: "15s", Description: "连接与单次工具调用超时（最大 10m）"},
		{Key: "tools_allow", Type: TStrings, Default: []string{}, Description: "仅启用这些工具（服务端原始名称）；留空表示全部"},
	}
	f := &fieldDef{
		Field:    Field{Key: "servers", Type: TObjects, Default: []any{}, Description: "MCP 服务列表（整体替换）；修改后立即重新连接", Items: itemFields},
		yamlPath: []string{"mcp", "servers"},
		get: func(c *config.Config) any {
			out := make([]MCPServerDTO, len(c.MCP.Servers))
			for i, m := range c.MCP.Servers {
				out[i] = dtoOf(m, true)
			}
			return out
		},
		set: func(c *config.Config, raw json.RawMessage) error {
			var items []MCPServerDTO
			if isNull(raw) || decodeStrict(raw, &items) != nil {
				return fmt.Errorf(keysMsg)
			}
			out := make([]config.MCPServer, 0, len(items))
			for _, d := range items {
				m, err := d.toConfig(c.MCP.Servers)
				if err != nil {
					return err
				}
				out = append(out, m)
			}
			c.MCP.Servers = out
			return nil
		},
		node: func(c *config.Config) (*yamlNode, error) {
			if len(c.MCP.Servers) == 0 {
				return &yamlNode{Kind: yamlSeq, Tag: "!!seq", Style: yamlFlow}, nil
			}
			return yamlOf(c.MCP.Servers)
		},
	}
	s := &section{
		Name: "mcp", Title: "MCP 工具",
		Description: "连接外部 MCP 服务，其工具以 mcp__<服务名>__<工具名> 提供给模型。修改后立即重新连接。",
		fields:      []*fieldDef{f},
	}
	s.validate = func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs) {
		if len(n.MCP.Servers) > maxMCPServers {
			fe["servers"] = fmt.Sprintf("at most %d servers", maxMCPServers)
			return
		}
		seen := map[string]bool{}
		for i, m := range n.MCP.Servers {
			for k, v := range config.ValidateMCPServer(m) {
				fe[fmt.Sprintf("servers[%d].%s", i, k)] = v
			}
			if seen[m.Name] {
				fe[fmt.Sprintf("servers[%d].name", i)] = "duplicate name"
			}
			seen[m.Name] = true
		}
	}
	s.apply = func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error) {
		a.MCP.Reload(ctx, mcpServers(n.MCP.Servers))
		return map[string]any{"servers": a.MCP.Status()}, nil, nil
	}
	return s
}

// TestMCP connects to one (possibly unsaved) server definition and lists
// its tools without registering anything. Masked header/env values keep the
// stored secret of the saved server with the same name.
func (a *App) TestMCP(ctx context.Context, d MCPServerDTO) (*mcp.TestResult, error) {
	m, err := d.toConfig(a.Config().MCP.Servers)
	fe := fieldErrs{}
	if err != nil {
		fe["timeout"] = err.Error()
	}
	for k, v := range config.ValidateMCPServer(m) {
		fe[k] = v
	}
	if len(fe) > 0 {
		return nil, &ValidationError{Fields: fe}
	}
	res := mcp.Test(ctx, mcpConfig(m))
	return &res, nil
}
