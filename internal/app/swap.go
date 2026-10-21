package app

import (
	"context"
	"sync"

	"github.com/andy/vox-mi-agent/internal/llm"
)

// SwapProvider is an llm.Provider whose backend, default model and
// temperature can be replaced at runtime (settings PUT).
type SwapProvider struct {
	mu    sync.RWMutex
	inner llm.Provider
	model string
	temp  float64
}

// NewSwapProvider wraps inner.
func NewSwapProvider(inner llm.Provider, model string, temp float64) *SwapProvider {
	return &SwapProvider{inner: inner, model: model, temp: temp}
}

// Set atomically replaces the backend and defaults.
func (s *SwapProvider) Set(inner llm.Provider, model string, temp float64) {
	s.mu.Lock()
	s.inner, s.model, s.temp = inner, model, temp
	s.mu.Unlock()
}

// Name implements llm.Provider.
func (s *SwapProvider) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Name()
}

// Stream implements llm.Provider, filling in model/temperature defaults.
func (s *SwapProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	s.mu.RLock()
	inner, model, temp := s.inner, s.model, s.temp
	s.mu.RUnlock()
	if req.Model == "" {
		req.Model = model
	}
	if req.Temperature == nil {
		t := temp
		req.Temperature = &t
	}
	return inner.Stream(ctx, req)
}

// Model returns the current default model name.
func (s *SwapProvider) Model() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model
}
