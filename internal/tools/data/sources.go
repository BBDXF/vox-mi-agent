// Package data implements the data-query tools query_json (jq),
// query_yaml (yq) and fuzzy_match (fzq) on top of internal/toolrunner.
// The LLM only ever names registered sources; it can never pass a path.
package data

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
)

// SourceSet is a swappable holder of the current *Sources (settings changes
// replace the registry without rebuilding the tools). The zero value holds
// no sources and is ready to use.
type SourceSet struct{ p atomic.Pointer[Sources] }

// NewSourceSet returns a set holding s.
func NewSourceSet(s *Sources) *SourceSet {
	ss := &SourceSet{}
	ss.Set(s)
	return ss
}

// Get returns the current registry (nil = none).
func (ss *SourceSet) Get() *Sources {
	if ss == nil {
		return nil
	}
	return ss.p.Load()
}

// Set atomically replaces the registry.
func (ss *SourceSet) Set(s *Sources) { ss.p.Store(s) }

// Source is a server-side registered data source (config.yaml `sources:`).
type Source struct {
	Name    string   // name the LLM uses
	File    string   // absolute path, never shown to the LLM
	Format  string   // fzq -f value: auto|lines|json|jsonl|yaml ("" = auto)
	Records string   // default fzq -r
	Keys    []string // default fzq -k
	Extract string   // fzq -x REGEX (server config only)
	Strip   []string // fzq --strip REGEX (server config only)
}

// Sources is the immutable registry.
type Sources struct {
	m     map[string]Source
	names []string
}

var validFormats = map[string]bool{"": true, "auto": true, "lines": true, "json": true, "jsonl": true, "yaml": true}

// NewSources validates and indexes srcs.
func NewSources(srcs []Source) (*Sources, error) {
	s := &Sources{m: map[string]Source{}}
	for _, src := range srcs {
		if src.Name == "" {
			return nil, fmt.Errorf("source with empty name")
		}
		if !filepath.IsAbs(src.File) {
			return nil, fmt.Errorf("source %q: file %q must be an absolute path", src.Name, src.File)
		}
		if !validFormats[src.Format] {
			return nil, fmt.Errorf("source %q: invalid format %q", src.Name, src.Format)
		}
		if _, dup := s.m[src.Name]; dup {
			return nil, fmt.Errorf("duplicate source %q", src.Name)
		}
		s.m[src.Name] = src
		s.names = append(s.names, src.Name)
	}
	sort.Strings(s.names)
	return s, nil
}

// Names returns the sorted source names. Safe on a nil receiver.
func (s *Sources) Names() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.names...)
}

// Get looks a source up; the error lists the available names.
func (s *Sources) Get(name string) (Source, error) {
	if s != nil {
		if src, ok := s.m[name]; ok {
			return src, nil
		}
	}
	avail := "(none)"
	if names := s.Names(); len(names) > 0 {
		avail = strings.Join(names, ", ")
	}
	return Source{}, fmt.Errorf("unknown source %q; available sources: %s", name, avail)
}
