package media

import (
	"context"
	"sync"
)

// AliasStore supplies user-maintained aliases keyed by track title.
type AliasStore interface {
	// Aliases returns title -> aliases.
	Aliases(ctx context.Context) (map[string][]string, error)
}

// MemAliasStore is an in-memory AliasStore (DB-backed one comes later).
type MemAliasStore struct {
	mu sync.RWMutex
	m  map[string][]string
}

// NewMemAliasStore returns a store seeded with a copy of m (may be nil).
func NewMemAliasStore(m map[string][]string) *MemAliasStore {
	s := &MemAliasStore{m: map[string][]string{}}
	for k, v := range m {
		s.m[k] = append([]string(nil), v...)
	}
	return s
}

// Set replaces the aliases of a title (empty => delete).
func (s *MemAliasStore) Set(title string, aliases []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(aliases) == 0 {
		delete(s.m, title)
		return
	}
	s.m[title] = append([]string(nil), aliases...)
}

// Aliases implements AliasStore; it returns a deep copy.
func (s *MemAliasStore) Aliases(context.Context) (map[string][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]string, len(s.m))
	for k, v := range s.m {
		out[k] = append([]string(nil), v...)
	}
	return out, nil
}
