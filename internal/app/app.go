// Package app wires the internal packages into a runnable application
// (shared by `app serve` and `app chat`).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/agent"
	"github.com/BBDXF/vox-mi-agent/internal/config"
	"github.com/BBDXF/vox-mi-agent/internal/llm"
	"github.com/BBDXF/vox-mi-agent/internal/logx"
	"github.com/BBDXF/vox-mi-agent/internal/media"
	"github.com/BBDXF/vox-mi-agent/internal/mediaurl"
	"github.com/BBDXF/vox-mi-agent/internal/speaker"
	"github.com/BBDXF/vox-mi-agent/internal/speaker/xiaomi"
	"github.com/BBDXF/vox-mi-agent/internal/store"
	"github.com/BBDXF/vox-mi-agent/internal/toolrunner"
	"github.com/BBDXF/vox-mi-agent/internal/tools"
	"github.com/BBDXF/vox-mi-agent/internal/tools/audio"
	"github.com/BBDXF/vox-mi-agent/internal/tools/data"
	"github.com/BBDXF/vox-mi-agent/internal/trace"
)

// Option customises Build.
type Option func(*options)

type options struct {
	provider llm.Provider
	logger   *slog.Logger
	level    *slog.LevelVar
	xiaomi   func(*xiaomi.Options)
}

// WithXiaomiOptions lets tests override the Xiaomi base URLs / HTTP client.
func WithXiaomiOptions(f func(*xiaomi.Options)) Option { return func(o *options) { o.xiaomi = f } }

// WithLevelVar links the runtime log level to lv (the handler of the logger
// passed via WithLogger must use the same LevelVar). Default: a private one.
func WithLevelVar(lv *slog.LevelVar) Option { return func(o *options) { o.level = lv } }

// WithProvider injects an LLM provider instead of the OpenAI client (tests).
// It is still wrapped by the swappable provider; a settings update replaces it.
func WithProvider(p llm.Provider) Option { return func(o *options) { o.provider = p } }

// WithLogger sets the logger (default slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// App is the wired application.
type App struct {
	Cfg       *config.Config // startup snapshot (paths made absolute); live values: Config()
	Log       *slog.Logger
	Store     *store.Store
	Runner    *toolrunner.Runner
	Library   *media.Library
	Syncer    *media.Syncer
	Index     *media.Index
	Scheduler *media.Scheduler
	Speaker   *speaker.Manager
	Hook      *speaker.Hook
	Mock      *speaker.Mock
	Tools     *tools.Registry
	Agent     *agent.Runtime
	Provider  *SwapProvider
	Health    ToolStatuses // startup self-check; live value: HealthStatus()
	Tracer    *trace.Recorder
	LevelVar  *slog.LevelVar // runtime log level (settings: logging.level)

	cfgMu sync.RWMutex // guards cfg (the live configuration)
	cfg   *config.Config
	updMu sync.Mutex // the single settings lock: serialises all settings updates

	healthMu sync.RWMutex
	sources  *data.SourceSet
	xmOpts   func(*xiaomi.Options)
	xmMu     sync.Mutex
	xm       *xiaomi.Driver
	hookOn   bool

	secret   []byte
	baseOnce sync.Once
	autoBase string
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
	a := &App{Cfg: c, Log: o.logger, Store: st, cfg: c.Clone(), LevelVar: o.level}
	if a.LevelVar == nil {
		a.LevelVar = new(slog.LevelVar)
		a.LevelVar.Set(logx.ParseLevel(c.Logging.Level))
	}
	if a.cfg.LLM.Provider == "" {
		a.cfg.LLM.Provider = llm.CustomPresetID
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
	// Tool timings are traced by the agent (traces table); the runner-level
	// record only goes to the debug log, not to audit_log.
	a.Runner.Auditor = func(rec toolrunner.Record) {
		a.Log.Debug("tool run", "tool", rec.Tool, "source", rec.Source, "exit_code", rec.ExitCode,
			"duration_ms", rec.Duration.Milliseconds(), "output_bytes", rec.OutputBytes,
			"truncated", rec.Truncated, "error", rec.Err)
	}

	sources, err := buildSources(c)
	if err != nil {
		return nil, err
	}
	a.sources = data.NewSourceSet(sources)

	a.Library = media.NewLibrary(media.SongsPath(c.DataDir))
	a.Syncer = media.NewSyncer(c, media.Deps{Runner: a.Runner, Store: st, Aliases: st, Library: a.Library, Log: a.Log})
	a.Index = media.NewIndex(c, a.Runner)
	a.Index.Log = a.Log
	a.Scheduler = media.NewScheduler(a.Syncer)

	if a.secret, err = mediaurl.LoadSecret(c.DataDir); err != nil {
		return nil, fmt.Errorf("app: server secret: %w", err)
	}
	a.xmOpts = o.xiaomi
	a.Mock = speaker.NewMock()
	a.Speaker = speaker.NewManager(a.Log)
	a.applySpeaker(a.cfg)
	reg := tools.NewRegistry()
	for _, t := range data.All(data.Deps{Runner: a.Runner, Live: a.sources, ToolsDir: c.Tools.Dir,
		BinPath: func(name string) string {
			var p string
			a.withConfig(func(c *config.Config) { p = toolPath(c, name) })
			return p
		}}) {
		reg.Register(t)
	}
	for _, t := range audio.All(audio.Deps{
		Searcher: a.Index, Library: a.Library, Player: a.Speaker, URLFor: a.TrackURL,
		Limit: c.Search.Limit, MinScore: int(c.Search.MinScore),
		Params: func() (limit, minScore int) {
			a.withConfig(func(c *config.Config) { limit, minScore = c.Search.Limit, int(c.Search.MinScore) })
			return
		},
	}) {
		reg.Register(t)
	}
	a.Tools = reg

	inner := o.provider
	if inner == nil {
		inner = newOpenAI(c.LLM)
	}
	a.Provider = NewSwapProvider(inner, c.LLM.Model, c.LLM.Temperature)
	a.Tracer = trace.NewRecorder(st, trace.PayloadDir(c.DataDir), a.Log, trace.WriterOptions{})
	a.Tracer.Configure(c.Logging)
	a.Tracer.P.SetSecrets(c.LLM.APIKey)
	a.Tracer.Start()
	a.Agent = agent.New(agent.Options{
		Provider: a.Provider, Tools: reg, Store: st, Logger: a.Log, Tracer: a.Tracer,
	})
	a.Hook = speaker.NewHook(speaker.HookDeps{Mgr: a.Speaker, Agent: a.Agent, Store: st, Tracer: a.Tracer, Log: a.Log,
		Music: &speaker.MusicDeps{Search: a.Index.Search, Tracks: a.Library.Tracks, Lookup: a.Library.Lookup, URLFor: a.TrackURL,
			MinScore: func() (v int) { a.withConfig(func(c *config.Config) { v = int(c.Search.MinScore) }); return }}})
	ok = true
	return a, nil
}

func newOpenAI(l config.LLM) llm.Provider {
	return llm.NewOpenAI(llm.Config{BaseURL: l.EffectiveBaseURL(), APIKey: l.APIKey, Model: l.Model, Timeout: l.Timeout,
		ExtraBody: l.EffectiveExtraBody()})
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

// Close flushes the trace writers and releases resources.
func (a *App) Close() error {
	if a.Hook != nil {
		a.Hook.Stop()
	}
	if a.Tracer != nil {
		a.Tracer.Close()
	}
	return a.Store.Close()
}

// Sources returns the live data-source registry (built-in media_library
// included, merged with the user sources).
func (a *App) Sources() *data.Sources { return a.sources.Get() }

// HealthStatus returns the latest tool self-check result.
func (a *App) HealthStatus() ToolStatuses {
	a.healthMu.RLock()
	defer a.healthMu.RUnlock()
	return a.Health
}

func (a *App) setHealth(h ToolStatuses) {
	a.healthMu.Lock()
	a.Health = h
	a.healthMu.Unlock()
}

// LLMConfig returns the current LLM settings (including the secret: never
// serialise it).
func (a *App) LLMConfig() config.LLM { return a.Config().LLM }

// LLMUpdate is a runtime LLM reconfiguration. Provider "" = current
// provider. APIKey "" keeps the stored key only when the provider is
// unchanged; switching provider with an empty key clears the stored key.
type LLMUpdate struct {
	Provider    string
	BaseURL     string
	Model       string
	APIKey      string
	Temperature float64
	// Timeout nil = unchanged.
	Timeout *time.Duration
	// SetExtraBody replaces llm.extra_body with ExtraBody (nil/empty = clear,
	// falling back to the provider preset default).
	ExtraBody    map[string]any
	SetExtraBody bool
}

// ErrInvalid marks a validation failure of user-supplied settings.
var ErrInvalid = errors.New("invalid settings")

// LLMTarget is a fully resolved LLM endpoint (provider, URL and key).
type LLMTarget struct {
	Provider string
	BaseURL  string
	APIKey   string
	Preset   llm.Preset
	// ExtraBody is the request-body extension that applies to this target.
	ExtraBody map[string]any
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
	eb := preset.ExtraBody
	if len(cur.ExtraBody) > 0 {
		eb = cur.ExtraBody
	}
	return LLMTarget{Provider: provider, BaseURL: baseURL, APIKey: apiKey, Preset: preset, ExtraBody: eb}, nil
}

// UpdateLLM validates u, persists it to <data>/config.yaml (the file is
// authoritative) and then swaps the live provider.
func (a *App) UpdateLLM(u LLMUpdate) error {
	a.updMu.Lock()
	defer a.updMu.Unlock()
	_, err := a.updateLLMLocked(u)
	return err
}

// updateLLMLocked is UpdateLLM with the settings lock held; it returns the
// keys whose value changed.
func (a *App) updateLLMLocked(u LLMUpdate) ([]string, error) {
	u.Model = strings.TrimSpace(u.Model)
	cur := a.Config().LLM
	tgt, err := a.resolveLocked(cur, u.Provider, u.BaseURL, u.APIKey)
	if err != nil {
		return nil, err
	}
	switch {
	case u.Model == "":
		return nil, fmt.Errorf("%w: model is required", ErrInvalid)
	case u.Temperature < 0 || u.Temperature > 2:
		return nil, fmt.Errorf("%w: temperature must be between 0 and 2", ErrInvalid)
	case tgt.Preset.KeyRequired && tgt.APIKey == "":
		return nil, fmt.Errorf("%w: %s requires an API key", ErrInvalid, tgt.Preset.Name)
	case u.Timeout != nil && (*u.Timeout < time.Second || *u.Timeout > 10*time.Minute):
		return nil, fmt.Errorf("%w: timeout must be between 1s and 10m", ErrInvalid)
	}
	var extra []yamlSet
	if u.Timeout != nil {
		extra = append(extra, yamlSet{[]string{"llm", "timeout"}, strNode(u.Timeout.String())})
	}
	if u.SetExtraBody {
		nd, err := mapNode(u.ExtraBody)
		if err != nil {
			return nil, fmt.Errorf("%w: extra_body: %v", ErrInvalid, err)
		}
		extra = append(extra, yamlSet{[]string{"llm", "extra_body"}, nd})
	}
	key := tgt.APIKey
	if err := persistLLM(a.Cfg.DataDir, tgt.Provider, tgt.BaseURL, u.Model, u.Temperature, &key, extra...); err != nil {
		return nil, fmt.Errorf("persist config: %w", err)
	}
	n := a.Config()
	nl := &n.LLM
	nl.Provider, nl.BaseURL, nl.APIKey = tgt.Provider, tgt.BaseURL, tgt.APIKey
	nl.Model, nl.Temperature = u.Model, u.Temperature
	if u.Timeout != nil {
		nl.Timeout = *u.Timeout
	}
	if u.SetExtraBody {
		nl.ExtraBody = u.ExtraBody
	}
	var changed []string
	add := func(k string, differs bool) {
		if differs {
			changed = append(changed, k)
		}
	}
	add("provider", cur.Provider != nl.Provider)
	add("base_url", cur.BaseURL != nl.BaseURL)
	add("api_key", cur.APIKey != nl.APIKey)
	add("model", cur.Model != nl.Model)
	add("temperature", cur.Temperature != nl.Temperature)
	add("timeout", cur.Timeout != nl.Timeout)
	add("extra_body", u.SetExtraBody && !reflect.DeepEqual(cur.ExtraBody, nl.ExtraBody))
	a.setConfig(n)
	a.Provider.Set(newOpenAI(*nl), nl.Model, nl.Temperature)
	a.Tracer.P.SetSecrets(nl.APIKey)
	a.auditSettings("llm", changed)
	a.Log.Info("llm reconfigured", "provider", nl.Provider, "base_url", nl.BaseURL, "model", nl.Model, "temperature", nl.Temperature)
	return changed, nil
}

// Secret returns the server secret used to sign media URLs.
func (a *App) Secret() []byte { return a.secret }

// publicBase returns the configured or auto-detected public base URL.
func (a *App) publicBase() (string, error) {
	cfg := a.Config()
	if b := cfg.Speaker.PublicBaseURL; b != "" {
		return b, nil
	}
	_, addr, err := config.ParseListen(a.Cfg.Server.Listen)
	if err != nil {
		return "", err
	}
	if network, _, _ := config.ParseListen(a.Cfg.Server.Listen); network != "tcp" {
		return "", errors.New("speaker.public_base_url must be set when listening on a unix socket")
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	a.baseOnce.Do(func() {
		b, err := mediaurl.AutoBase(port)
		if err != nil {
			a.Log.Warn("media url: auto-detect failed", "err", err)
			return
		}
		a.autoBase = b
		a.Log.Warn("speaker.public_base_url is empty; using auto-detected address. "+
			"In Docker bridge mode this is likely unreachable by the speaker: set speaker.public_base_url",
			"base_url", b)
	})
	if a.autoBase == "" {
		return "", errors.New("cannot determine public base URL; set speaker.public_base_url")
	}
	return a.autoBase, nil
}

// TrackURL builds a signed, expiring media URL for t.
func (a *App) TrackURL(t media.Track) (string, error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(t.Path)), ".")
	if ext == "" {
		return "", errors.New("track has no file extension")
	}
	base, err := a.publicBase()
	if err != nil {
		return "", err
	}
	if bp := a.Cfg.Server.BasePath; bp != "" && !strings.HasSuffix(base, bp) {
		base += strings.TrimRight(bp, "/")
	}
	return mediaurl.Sign(a.secret, base, t.ID, ext, time.Now().Add(a.Config().Speaker.URLTTL)), nil
}
