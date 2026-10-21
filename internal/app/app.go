// Package app wires the internal packages into a runnable application
// (shared by `app serve` and `app chat`).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/media"
	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/toolrunner"
	"github.com/andy/vox-mi-agent/internal/tools"
	"github.com/andy/vox-mi-agent/internal/tools/audio"
	"github.com/andy/vox-mi-agent/internal/tools/data"
)

// Option customises Build.
type Option func(*options)

type options struct {
	provider llm.Provider
	logger   *slog.Logger
}

// WithProvider injects an LLM provider instead of the OpenAI client (tests).
// It is still wrapped by the swappable provider; a settings update replaces it.
func WithProvider(p llm.Provider) Option { return func(o *options) { o.provider = p } }

// WithLogger sets the logger (default slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// App is the wired application.
type App struct {
	Cfg       *config.Config // effective config (paths made absolute); LLM section: use LLMConfig
	Log       *slog.Logger
	Store     *store.Store
	Runner    *toolrunner.Runner
	Library   *media.Library
	Syncer    *media.Syncer
	Index     *media.Index
	Scheduler *media.Scheduler
	Player    *audio.LogPlayer
	Tools     *tools.Registry
	Agent     *agent.Runtime
	Provider  *SwapProvider
	Health    ToolStatuses

	mu  sync.Mutex // guards llmCfg and serialises UpdateLLM
	llm config.LLM
}

// Build constructs the application from cfg. Tool self-check failures are
// recorded in Health and never abort startup.
func Build(cfg *config.Config, opts ...Option) (*App, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	c, err := normalize(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("app: data dir: %w", err)
	}
	st, err := store.Open(c.DBPath())
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: c, Log: o.logger, Store: st, llm: c.LLM}
	if a.llm.Provider == "" {
		a.llm.Provider = llm.CustomPresetID
	}
	ok := false
	defer func() {
		if !ok {
			st.Close()
		}
	}()

	a.Health = SelfCheck(context.Background(), c.Tools.FzqPath, c.Tools.JqPath, c.Tools.YqPath)
	for name, ts := range map[string]ToolStatus{"fzq": a.Health.Fzq, "jq": a.Health.Jq, "yq": a.Health.Yq} {
		if ts.OK {
			a.Log.Info("tool ok", "tool", name, "path", ts.Path, "version", ts.Version)
		} else {
			a.Log.Warn("tool unavailable", "tool", name, "path", ts.Path, "err", ts.Error)
		}
	}

	a.Runner = toolrunner.New()
	a.Runner.Auditor = func(rec toolrunner.Record) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.Audit(ctx, "tool", rec.Tool, map[string]any{
			"source": rec.Source, "query": rec.Query, "exit_code": rec.ExitCode,
			"duration_ms": rec.Duration.Milliseconds(), "output_bytes": rec.OutputBytes,
			"truncated": rec.Truncated, "error": rec.Err,
		}); err != nil {
			a.Log.Warn("audit tool run", "err", err)
		}
	}

	srcs := make([]data.Source, 0, len(c.Sources))
	for _, s := range c.Sources {
		srcs = append(srcs, data.Source{
			Name: s.Name, File: c.SourcePath(s), Format: s.Format, Records: s.Records,
			Keys: s.Keys, Extract: s.Extract, Strip: s.Strip,
		})
	}
	sources, err := data.NewSources(srcs)
	if err != nil {
		return nil, fmt.Errorf("app: sources: %w", err)
	}

	a.Library = media.NewLibrary(media.SongsPath(c.DataDir))
	a.Syncer = media.NewSyncer(c, media.Deps{Runner: a.Runner, Store: st, Library: a.Library, Log: a.Log})
	a.Index = media.NewIndex(c, a.Runner)
	a.Index.Log = a.Log
	a.Scheduler = media.NewScheduler(a.Syncer)

	a.Player = &audio.LogPlayer{}
	player := &loggingPlayer{LogPlayer: a.Player, log: a.Log}
	reg := tools.NewRegistry()
	for _, t := range data.All(data.Deps{Runner: a.Runner, Sources: sources, ToolsDir: c.Tools.Dir}) {
		reg.Register(t)
	}
	for _, t := range audio.All(audio.Deps{
		Searcher: a.Index, Library: a.Library, Player: player,
		Limit: c.Search.Limit, MinScore: int(c.Search.MinScore),
	}) {
		reg.Register(t)
	}
	a.Tools = reg

	inner := o.provider
	if inner == nil {
		inner = newOpenAI(c.LLM)
	}
	a.Provider = NewSwapProvider(inner, c.LLM.Model, c.LLM.Temperature)
	a.Agent = agent.New(agent.Options{
		Provider: a.Provider, Tools: reg, Store: st, Logger: a.Log,
	})
	ok = true
	return a, nil
}

func newOpenAI(l config.LLM) llm.Provider {
	return llm.NewOpenAI(llm.Config{BaseURL: l.EffectiveBaseURL(), APIKey: l.APIKey, Model: l.Model, Timeout: l.Timeout})
}

// normalize returns a copy of cfg with filesystem paths made absolute (the
// tool runner and data sources require absolute paths).
func normalize(cfg *config.Config) (*config.Config, error) {
	c := *cfg
	abs := func(p string) (string, error) {
		if p == "" || filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Abs(p)
	}
	var err error
	for _, p := range []*string{&c.DataDir, &c.Tools.Dir, &c.Tools.FzqPath, &c.Tools.JqPath, &c.Tools.YqPath} {
		if *p, err = abs(*p); err != nil {
			return nil, err
		}
	}
	if c.Tools.Dir == "" {
		c.Tools.Dir = config.DefaultToolsDir
	}
	for name, p := range map[string]*string{"fzq": &c.Tools.FzqPath, "jq": &c.Tools.JqPath, "yq": &c.Tools.YqPath} {
		if *p == "" {
			*p = filepath.Join(c.Tools.Dir, name)
		}
	}
	return &c, nil
}

// Close releases resources.
func (a *App) Close() error { return a.Store.Close() }

// LLMConfig returns the current LLM settings (including the secret: never
// serialise it).
func (a *App) LLMConfig() config.LLM {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.llm
}

// LLMUpdate is a runtime LLM reconfiguration. Provider "" = current
// provider. APIKey "" keeps the stored key only when the provider is
// unchanged; switching provider with an empty key clears the stored key.
type LLMUpdate struct {
	Provider    string
	BaseURL     string
	Model       string
	APIKey      string
	Temperature float64
}

// ErrInvalid marks a validation failure of user-supplied settings.
var ErrInvalid = errors.New("invalid settings")

// LLMTarget is a fully resolved LLM endpoint (provider, URL and key).
type LLMTarget struct {
	Provider string
	BaseURL  string
	APIKey   string
	Preset   llm.Preset
}

// ResolveLLM resolves a user-supplied (provider, base_url, api_key) triple
// against the preset table and the current settings. Provider "" means the
// current provider. An empty api_key falls back to the stored key only when
// the provider equals the current one.
func (a *App) ResolveLLM(provider, baseURL, apiKey string) (LLMTarget, error) {
	return a.resolveLocked(a.LLMConfig(), provider, baseURL, apiKey)
}

func (a *App) resolveLocked(cur config.LLM, provider, baseURL, apiKey string) (LLMTarget, error) {
	curProv := cur.Provider
	if curProv == "" {
		curProv = llm.CustomPresetID
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = curProv
	}
	preset, ok := llm.PresetByID(provider)
	if !ok {
		return LLMTarget{}, fmt.Errorf("%w: unknown provider %q", ErrInvalid, provider)
	}
	baseURL = strings.TrimSpace(baseURL)
	switch {
	case !preset.BaseURLEditable && preset.ID != llm.CustomPresetID:
		baseURL = preset.BaseURL
	case baseURL == "" && preset.BaseURL != "":
		baseURL = preset.BaseURL
	case baseURL == "" && provider == curProv:
		baseURL = cur.BaseURL
	}
	pu, err := url.Parse(baseURL)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return LLMTarget{}, fmt.Errorf("%w: base_url must be an http(s) URL", ErrInvalid)
	}
	if apiKey == "" && provider == curProv {
		apiKey = cur.APIKey
	}
	return LLMTarget{Provider: provider, BaseURL: baseURL, APIKey: apiKey, Preset: preset}, nil
}

// UpdateLLM validates u, persists it to <data>/config.yaml (the file is
// authoritative) and then swaps the live provider.
func (a *App) UpdateLLM(u LLMUpdate) error {
	u.Model = strings.TrimSpace(u.Model)
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.llm
	tgt, err := a.resolveLocked(cur, u.Provider, u.BaseURL, u.APIKey)
	if err != nil {
		return err
	}
	switch {
	case u.Model == "":
		return fmt.Errorf("%w: model is required", ErrInvalid)
	case u.Temperature < 0 || u.Temperature > 2:
		return fmt.Errorf("%w: temperature must be between 0 and 2", ErrInvalid)
	case tgt.Preset.KeyRequired && tgt.APIKey == "":
		return fmt.Errorf("%w: %s requires an API key", ErrInvalid, tgt.Preset.Name)
	}
	key := tgt.APIKey
	if err := persistLLM(a.Cfg.DataDir, tgt.Provider, tgt.BaseURL, u.Model, u.Temperature, &key); err != nil {
		return fmt.Errorf("persist config: %w", err)
	}
	n := cur
	n.Provider, n.BaseURL, n.APIKey = tgt.Provider, tgt.BaseURL, tgt.APIKey
	n.Model, n.Temperature = u.Model, u.Temperature
	a.llm = n
	a.Provider.Set(newOpenAI(n), n.Model, n.Temperature)
	a.Log.Info("llm reconfigured", "provider", n.Provider, "base_url", n.BaseURL, "model", n.Model, "temperature", n.Temperature)
	return nil
}

// loggingPlayer logs and records play/volume calls until a real speaker exists.
type loggingPlayer struct {
	*audio.LogPlayer
	log *slog.Logger
}

func (p *loggingPlayer) Play(ctx context.Context, t media.Track) error {
	p.log.Info("speaker(log): play", "id", t.ID, "title", t.Title, "artist", t.Artist)
	return p.LogPlayer.Play(ctx, t)
}

func (p *loggingPlayer) SetVolume(ctx context.Context, v int) error {
	p.log.Info("speaker(log): set volume", "volume", v)
	return p.LogPlayer.SetVolume(ctx, v)
}
