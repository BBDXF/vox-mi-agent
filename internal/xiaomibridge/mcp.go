package xiaomibridge

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultWaitInput = 30 * time.Second
	maxWaitInput     = 120 * time.Second
)

// httpEquivalent maps each MCP tool to the REST call doing the same thing.
var httpEquivalent = map[string]struct{ method, path, body string }{
	"speak":            {"POST", "/v1/tts", `{"text":"你好","wait":false}`},
	"stop":             {"POST", "/v1/stop", ""},
	"get_latest_input": {"GET", "/v1/asr/latest", ""},
	"wait_for_input":   {"GET", "/v1/asr/stream", ""},
	"get_status":       {"GET", "/v1/status", ""},
	"play_url":         {"POST", "/v1/play-url", `{"url":"http://192.168.1.10:8080/a.mp3"}`},
	"abort_xiaoai":     {"POST", "/v1/abort-xiaoai", ""},
}

type latestOut struct {
	Input  *Event `json:"input" jsonschema:"latest final user input, null if none since the bridge started"`
	Finals int64  `json:"finals" jsonschema:"number of final inputs since the bridge started"`
}

type waitIn struct {
	AfterSeq       int64 `json:"after_seq,omitempty" jsonschema:"return the first input with seq greater than this (use the seq of the last input you handled); 0 = the latest one or the next"`
	TimeoutSeconds int   `json:"timeout_seconds,omitempty" jsonschema:"how long to wait, 1-120, default 30"`
}

type waitOut struct {
	Input    *Event `json:"input" jsonschema:"the user input, null on timeout"`
	TimedOut bool   `json:"timed_out"`
}

type urlIn struct {
	URL string `json:"url" jsonschema:"http(s) audio URL reachable from the speaker"`
}

type okOut struct {
	OK bool `json:"ok"`
}

func (b *Bridge) newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "xiaomi-bridge", Version: buildinfo.Version}, &mcp.ServerOptions{
		Instructions: "Controls a Xiaomi smart speaker. User speech recognized by the speaker arrives through wait_for_input / get_latest_input; answer with speak. speak interrupts the previous speak.",
	})
	mcp.AddTool(s, &mcp.Tool{Name: "speak", Description: "Speak text on the speaker (text-to-speech). Interrupts any previous speak. With wait=true returns after playback ended."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in ttsIn) (*mcp.CallToolResult, ttsOut, error) {
			out, err := b.speak(ctx, in)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "stop", Description: "Stop playback now: kills the bridge's speech and pauses/stops the speaker's players."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, stopOut, error) {
			return nil, b.stop(ctx), nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_latest_input", Description: "Return the latest thing the user said to the speaker (final speech recognition result)."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, latestOut, error) {
			latest, finals := b.hub.Latest()
			return nil, latestOut{Input: latest, Finals: finals}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "wait_for_input", Description: "Wait for the user's next utterance (long poll). Pass after_seq = seq of the last input you handled."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in waitIn) (*mcp.CallToolResult, waitOut, error) {
			d := time.Duration(in.TimeoutSeconds) * time.Second
			if d <= 0 {
				d = defaultWaitInput
			}
			d = min(d, maxWaitInput)
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			ev, ok := b.hub.WaitFinal(ctx, in.AfterSeq)
			if !ok {
				return nil, waitOut{TimedOut: true}, nil
			}
			return nil, waitOut{Input: &ev}, nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "get_status", Description: "Internal state: instruction log, latest input, push delivery, current speech, player status."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusReport, error) {
			return nil, b.Status(ctx), nil
		})
	mcp.AddTool(s, &mcp.Tool{Name: "play_url", Description: "Play an audio URL on the speaker's media player."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in urlIn) (*mcp.CallToolResult, okOut, error) {
			if !httpURL(in.URL) {
				return nil, okOut{}, fmt.Errorf("url must be http(s)")
			}
			return nil, okOut{OK: true}, b.dev.PlayURL(ctx, in.URL)
		})
	mcp.AddTool(s, &mcp.Tool{Name: "abort_xiaoai", Description: "Restart XiaoAi's voice service to drop its current dialog. Takes 1-2 s; may not stop a native reply that already started playing."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, okOut, error) {
			return nil, okOut{OK: true}, b.dev.AbortXiaoAI(context.WithoutCancel(ctx))
		})
	return s
}

func (b *Bridge) mcpHandler() http.Handler {
	s := b.newMCPServer()
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: b.log})
}

type toolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
	HTTP        string `json:"http"`
	Curl        string `json:"curl"`
}

// tools lists the MCP tools as the protocol reports them (schemas included),
// via an in-memory session so there is a single definition.
func (b *Bridge) tools(ctx context.Context) ([]toolInfo, error) {
	b.mcpMu.Lock()
	defer b.mcpMu.Unlock()
	if b.mcpTools != nil {
		return b.mcpTools, nil
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := b.newMCPServer().Connect(ctx, st, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "descriptor"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	tools := make([]toolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		tools = append(tools, toolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	b.mcpTools = tools
	return tools, nil
}

// mcpDescriptor returns a ready-to-paste MCP client config (with the caller's
// own credentials), the tools and the equivalent curl commands.
func (b *Bridge) mcpDescriptor(w http.ResponseWriter, r *http.Request) {
	tools, err := b.tools(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, pass, _ := r.BasicAuth()
	authz := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	base := "http://" + r.Host
	out := make([]toolInfo, len(tools))
	for i, t := range tools {
		// t is a copy: the cached list stays untouched.
		if h, ok := httpEquivalent[t.Name]; ok {
			t.HTTP = h.method + " " + h.path
			t.Curl = fmt.Sprintf("curl -u '%s:<password>' -X %s %s%s", user, h.method, base, h.path)
			if h.body != "" {
				t.Curl += " -H 'Content-Type: application/json' -d '" + h.body + "'"
			}
			if t.Name == "wait_for_input" {
				t.Curl = fmt.Sprintf("curl -N -u '%s:<password>' %s/v1/asr/stream", user, base)
			}
		}
		out[i] = t
	}
	reply(w, http.StatusOK, map[string]any{
		"name":    "xiaomi-bridge",
		"version": buildinfo.Version,
		"mcpServers": map[string]any{
			"xiaomi-bridge": map[string]any{
				"type":    "http",
				"url":     base + "/mcp",
				"headers": map[string]string{"Authorization": authz},
			},
		},
		"tools": out,
	})
}
