// Package tools defines the contract between the agent runtime and its tools.
// Tool schemas are JSON-Schema objects aligned with MCP tool definitions.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Tool is a capability the LLM can invoke.
type Tool interface {
	Name() string
	Description() string
	// Schema returns the JSON Schema of the input object.
	Schema() json.RawMessage
	// Call executes the tool. args is the raw JSON object produced by the LLM.
	// The returned value is marshalled to JSON and fed back to the LLM as data.
	Call(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry holds the tools available to a session.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range ts {
		r.Register(t)
	}
	return r
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns tools sorted by name (stable prompt ordering).
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Call dispatches by name.
func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return t.Call(ctx, args)
}
