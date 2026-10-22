package app

import (
	"context"
	"sync"

	"github.com/BBDXF/vox-mi-agent/internal/llm"
)

// SwapProvider is an llm.Provider whose backend, default model can be replaced at runtime (settings PUT).
type SwapProvider struct {
	mu    sync.RWMutex
	inner llm.Provider
	model string
}

// NewSwapProvider wraps inner.
func NewSwapProvider(inner llm.Provider, model string) *SwapProvider {
	return &SwapProvider{inner: inner, model: model}
}

// Set atomically replaces the backend and defaults.
func (s *SwapProvider) Set(inner llm.Provider, model string) {
	s.mu.Lock()
	s.inner, s.model = inner, model
	s.mu.Unlock()
}

// Name implements llm.Provider.
func (s *SwapProvider) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Name()
}

// Stream implements llm.Provider, filling in model default.
func (s *SwapProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	s.mu.RLock()
	inner, model := s.inner, s.model
	s.mu.RUnlock()
	if req.Model == "" {
		req.Model = model
	}
	return inner.Stream(ctx, req)
}

// Model returns the current default model name.
func (s *SwapProvider) Model() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model
}
