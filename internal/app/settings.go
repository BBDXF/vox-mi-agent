package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/BBDXF/vox-mi-agent/internal/config"
)

// ErrUnknownSection is returned for a section name that does not exist.
var ErrUnknownSection = errors.New("unknown settings section")

// ValidationError lists per-field problems of a settings update. Keys are
// field keys ("search.limit" style for nested keys), list elements
// ("items[2].name"), or — for media folders — the offending path as submitted.
// It matches ErrInvalid.
type ValidationError struct {
	Fields map[string]string
}

// MediaValidationError is the former name of ValidationError.
type MediaValidationError = ValidationError

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Fields[k]))
	}
	return ErrInvalid.Error() + ": " + strings.Join(parts, "; ")
}

// Is makes errors.Is(err, ErrInvalid) true.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalid }

type fieldErrs map[string]string

type keySet map[string]bool

func (k keySet) any(keys ...string) bool {
	for _, x := range keys {
		if k[x] {
			return true
		}
	}
	return false
}

// PutResult is the outcome of a settings update.
type PutResult struct {
	Section         string         `json:"section"`
	RestartRequired bool           `json:"restart_required"`
	Applied         map[string]any `json:"applied"`
	Changed         []string       `json:"changed"`
	// SyncStarted is set by sections that trigger a media sync (media,
	// aliases): true when it started now, false when none was needed or one
	// was already running (a follow-up sync is then queued).
	SyncStarted *bool `json:"sync_started,omitempty"`
}

// section is one editable settings section.
type section struct {
	Name            string
	Title           string // 中文标题
	Description     string
	RestartRequired bool
	fields          []*fieldDef

	// validate runs after decoding on the candidate config n; it may normalise
	// n. provided: keys present in the request; changed: keys whose value
	// differs from old.
	validate func(a *App, old, n *config.Config, provided, changed keySet, fe fieldErrs)
	// apply makes n live (called after persisting, may adjust n, e.g. resolve
	// paths) and returns a description of what was applied.
	apply func(ctx context.Context, a *App, old, n *config.Config, changed keySet) (map[string]any, *bool, error)

	// custom handlers (not config.yaml-backed or with special semantics)
	getFn func(ctx context.Context, a *App) (map[string]any, error)
	putFn func(ctx context.Context, a *App, body map[string]json.RawMessage) (*PutResult, error)
}

func (s *section) field(key string) *fieldDef {
	for _, f := range s.fields {
		if f.Key == key {
			return f
		}
	}
	return nil
}

func (s *section) hasPrefix(prefix string) bool {
	for _, f := range s.fields {
		if strings.HasPrefix(f.Key, prefix+".") {
			return true
		}
	}
	return false
}

// flatten turns a (possibly nested) request body into dotted field keys.
func (s *section) flatten(prefix string, body map[string]json.RawMessage, out map[string]json.RawMessage) {
	for k, v := range body {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if s.field(key) == nil && s.hasPrefix(key) {
			var sub map[string]json.RawMessage
			if json.Unmarshal(v, &sub) == nil && sub != nil {
				s.flatten(key, sub, out)
				continue
			}
		}
		out[key] = v
	}
}

func setNested(m map[string]any, key string, v any) {
	parts := strings.Split(key, ".")
	for _, p := range parts[:len(parts)-1] {
		sub, ok := m[p].(map[string]any)
		if !ok {
			sub = map[string]any{}
			m[p] = sub
		}
		m = sub
	}
	m[parts[len(parts)-1]] = v
}

func (s *section) values(c *config.Config) map[string]any {
	out := map[string]any{}
	for _, f := range s.fields {
		setNested(out, f.Key, f.get(c))
	}
	return out
}

// SectionSchema is one entry of the schema document.
type SectionSchema struct {
	Name            string  `json:"name"`
	Title           string  `json:"title"`
	Description     string  `json:"description,omitempty"`
	RestartRequired bool    `json:"restart_required"`
	Fields          []Field `json:"fields"`
}

var defaultsCfg = config.Defaults()

// Schema describes every settings section and field (with defaults).
func Schema() []SectionSchema {
	out := make([]SectionSchema, 0, len(sections))
	for _, s := range sections {
		ss := SectionSchema{Name: s.Name, Title: s.Title, Description: s.Description,
			RestartRequired: s.RestartRequired, Fields: make([]Field, 0, len(s.fields))}
		for _, f := range s.fields {
			fd := f.Field
			if fd.Default == nil && f.get != nil {
				fd.Default = f.get(defaultsCfg)
			}
			ss.Fields = append(ss.Fields, fd)
		}
		out = append(out, ss)
	}
	return out
}

// SectionNames lists the editable sections in display order.
func SectionNames() []string {
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		out = append(out, s.Name)
	}
	return out
}

func lookupSection(name string) (*section, error) {
	for _, s := range sections {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownSection, name)
}

// Config returns a deep copy of the live configuration. Handlers must use
// this (not the startup snapshot a.Cfg).
func (a *App) Config() *config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg.Clone()
}

// withConfig runs f on the live config under the read lock (f must not
// retain or modify it).
func (a *App) withConfig(f func(*config.Config)) {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	f(a.cfg)
}

func (a *App) setConfig(c *config.Config) {
	a.cfgMu.Lock()
	a.cfg = c
	a.cfgMu.Unlock()
}

// GetSection returns the current values of a section (secrets masked).
func (a *App) GetSection(ctx context.Context, name string) (map[string]any, error) {
	s, err := lookupSection(name)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if s.getFn != nil {
		if out, err = s.getFn(ctx, a); err != nil {
			return nil, err
		}
	} else {
		out = s.values(a.Config())
	}
	if locked := s.envLocked(a.Config()); len(locked) > 0 && out != nil {
		out["env_locked"] = locked
	}
	return out, nil
}

// envLocked lists the section's fields overridden by environment variables.
func (s *section) envLocked(c *config.Config) map[string]string {
	out := map[string]string{}
	for _, f := range s.fields {
		if env, ok := c.EnvLocked[strings.Join(f.path(s.Name), ".")]; ok {
			out[f.Key] = env
		}
	}
	return out
}

// checkLocked rejects changes to env-managed or read-only fields; unchanged
// values are accepted (forms resend the whole section).
func (s *section) checkLocked(cur *config.Config, body map[string]json.RawMessage) error {
	flat := map[string]json.RawMessage{}
	s.flatten("", body, flat)
	locked := s.envLocked(cur)
	fe := fieldErrs{}
	for k, raw := range flat {
		f := s.field(k)
		if f == nil || f.set == nil {
			continue
		}
		msg := ""
		if env, ok := locked[k]; ok {
			msg = "由环境变量 " + env + " 管理"
		} else if f.ReadOnly {
			msg = "只读：请通过环境变量或配置文件设置"
		} else {
			continue
		}
		if f.Secret && string(bytes.TrimSpace(raw)) == `""` {
			continue
		}
		n := cur.Clone()
		if err := f.set(n, raw); err != nil || !reflect.DeepEqual(f.get(cur), f.get(n)) {
			fe[k] = msg
		}
	}
	if len(fe) > 0 {
		return &ValidationError{Fields: fe}
	}
	return nil
}

// PutSection validates, persists and applies a (partial) update of one
// section: only the keys present in body change. Nested keys may be sent
// nested ({"shell":{"enabled":true}}) or dotted. Updates are serialised by
// the single settings lock.
func (a *App) PutSection(ctx context.Context, name string, body map[string]json.RawMessage) (*PutResult, error) {
	s, err := lookupSection(name)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, &ValidationError{Fields: map[string]string{"": "no settings provided"}}
	}
	a.updMu.Lock()
	defer a.updMu.Unlock()
	if err := s.checkLocked(a.Config(), body); err != nil {
		return nil, err
	}
	if s.putFn != nil {
		return s.putFn(ctx, a, body)
	}
	old := a.Config()
	n := old.Clone()

	flat := map[string]json.RawMessage{}
	s.flatten("", body, flat)
	fe := fieldErrs{}
	provided := keySet{}
	var keys []string
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := s.field(k)
		if f == nil {
			fe[k] = "unknown field"
			continue
		}
		if err := f.set(n, flat[k]); err != nil {
			fe[k] = err.Error()
			continue
		}
		provided[k] = true
	}
	changedOf := func() keySet {
		ch := keySet{}
		for k := range provided {
			f := s.field(k)
			if !reflect.DeepEqual(f.get(old), f.get(n)) {
				ch[k] = true
			}
		}
		return ch
	}
	if s.validate != nil {
		s.validate(a, old, n, provided, changedOf(), fe)
	}
	if len(fe) > 0 {
		return nil, &ValidationError{Fields: fe}
	}
	changed := changedOf()

	var sets []yamlSet
	for _, k := range keys {
		f := s.field(k)
		nd, err := f.node(n)
		if err != nil {
			return nil, fmt.Errorf("encode %s.%s: %w", s.Name, k, err)
		}
		sets = append(sets, yamlSet{Path: f.path(s.Name), Node: nd})
	}
	if err := persistSets(a.Cfg.DataDir, sets); err != nil {
		return nil, fmt.Errorf("persist config: %w", err)
	}

	res := &PutResult{Section: s.Name, Applied: map[string]any{}, Changed: sortedKeys(changed)}
	for k := range changed {
		if s.RestartRequired || s.field(k).RestartRequired {
			res.RestartRequired = true
		}
	}
	if s.apply != nil && !s.RestartRequired {
		applied, sync, err := s.apply(ctx, a, old, n, changed)
		if err != nil {
			return nil, fmt.Errorf("apply %s: %w", s.Name, err)
		}
		if applied != nil {
			res.Applied = applied
		}
		res.SyncStarted = sync
	}
	a.setConfig(n)
	a.auditSettings(s.Name, res.Changed)
	a.Log.Info("settings updated", "section", s.Name, "changed", res.Changed, "restart_required", res.RestartRequired)
	return res, nil
}

func sortedKeys(k keySet) []string {
	out := make([]string, 0, len(k))
	for x := range k {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// auditSettings records a settings change (key names only, never values).
func (a *App) auditSettings(sec string, keys []string) {
	if len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5e9)
	defer cancel()
	if err := a.Store.Audit(ctx, "settings", "admin", map[string]any{"section": sec, "keys": keys}); err != nil {
		a.Log.Warn("audit settings", "err", err)
	}
}
