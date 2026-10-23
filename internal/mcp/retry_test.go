package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BBDXF/vox-mi-agent/internal/tools"
)

func memDial(t *testing.T) func(context.Context, ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
	return func(ctx context.Context, c ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
		srv := sdk.NewServer(&sdk.Implementation{Name: "s", Version: "1"}, nil)
		sdk.AddTool(srv, &sdk.Tool{Name: "echo", InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, r *sdk.CallToolRequest, in map[string]any) (*sdk.CallToolResult, any, error) {
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "hi"}}}, nil, nil
			})
		t1, t2 := sdk.NewInMemoryTransports()
		if _, err := srv.Connect(ctx, t1, nil); err != nil {
			return nil, nil, err
		}
		return connect(ctx, c, t2)
	}
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestRetryRegistersAfterFailure(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg, nil)
	m.Backoff = func(int) time.Duration { return 10 * time.Millisecond }
	good := memDial(t)
	var n atomic.Int32
	m.dial = func(ctx context.Context, c ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
		if n.Add(1) < 3 {
			return nil, nil, errors.New("boom")
		}
		return good(ctx, c)
	}
	m.Reload(context.Background(), []Server{{ServerConfig: ServerConfig{Name: "f", Transport: "stdio"}, Enabled: true}})
	if st := m.Status()[0]; st.Connected {
		t.Fatal("should start disconnected")
	}
	waitFor(t, func() bool { return m.Status()[0].Connected })
	if _, ok := reg.Get("mcp__f__echo"); !ok {
		t.Fatal("tool not registered")
	}
	if st := m.Status()[0]; st.Error != "" || st.Retrying || len(st.Tools) != 1 {
		t.Fatalf("bad status %+v", st)
	}
	// session drop -> unregistered
	m.mu.Lock()
	cs := m.sessions[0]
	m.mu.Unlock()
	cs.Close()
	waitFor(t, func() bool { _, ok := reg.Get("mcp__f__echo"); return !ok })
	waitFor(t, func() bool { return m.Status()[0].Connected })
	m.Close()
}

func TestReloadCancelsRetry(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg, nil)
	m.Backoff = func(int) time.Duration { return 30 * time.Millisecond }
	var n atomic.Int32
	m.dial = func(ctx context.Context, c ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
		n.Add(1)
		return nil, nil, errors.New("boom")
	}
	m.Reload(context.Background(), []Server{{ServerConfig: ServerConfig{Name: "f", Transport: "stdio"}, Enabled: true}})
	waitFor(t, func() bool { return n.Load() >= 2 && m.Status()[0].Retrying })
	m.Reload(context.Background(), nil)
	time.Sleep(50 * time.Millisecond)
	c := n.Load()
	time.Sleep(150 * time.Millisecond)
	if n.Load() != c {
		t.Fatal("retry continued after Reload")
	}
}
