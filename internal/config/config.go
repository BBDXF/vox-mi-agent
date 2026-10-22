// Package config loads the vox-mi-agent configuration.
//
// Priority (later overrides earlier):
//
//	built-in defaults -> <dataDir>/config.yaml -> VOX_* environment variables
//	-> (future) DB settings layer.
//
// config.yaml is authoritative for every key. All user-facing sections are
// editable at runtime via /api/settings/{section}, which rewrites the file
// (comments preserved) and applies the change live where possible (see
// internal/app). The SQLite `settings` table holds runtime state only; the
// alias table lives in its own DB table.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/andy/vox-mi-agent/internal/mediaurl"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"gopkg.in/yaml.v3"

	"github.com/andy/vox-mi-agent/internal/llm"
)

//go:embed default.yaml
var defaultYAML []byte

// DefaultYAML returns the embedded default config file contents.
func DefaultYAML() []byte { return append([]byte(nil), defaultYAML...) }

const (
	DefaultDataDir  = "/data"
	DefaultToolsDir = "/usr/local/bin"
	// FileName is the config file name inside the data dir.
	FileName = "config.yaml"
)

type Config struct {
	DataDir string `yaml:"-" json:"data_dir"`

	Server  Server   `yaml:"server" json:"server"`
	LLM     LLM      `yaml:"llm" json:"llm"`
	Tools   Tools    `yaml:"tools" json:"tools"`
	Sources []Source `yaml:"sources" json:"sources"`
	Media   Media    `yaml:"media" json:"media"`
	Search  Search   `yaml:"search" json:"search"`
	Speaker Speaker  `yaml:"speaker" json:"speaker"`
	Logging Logging  `yaml:"logging" json:"logging"`
}

// Logging configures runtime logging, tracing and the debug payload log.
type Logging struct {
	// Level is debug|info|warn|error (applies live).
	Level string `yaml:"level" json:"level"`
	// TraceRetentionDays / TraceMaxRows bound the traces table.
	TraceRetentionDays int `yaml:"trace_retention_days" json:"trace_retention_days"`
	TraceMaxRows       int `yaml:"trace_max_rows" json:"trace_max_rows"`
	// DebugPayloads enables the full request/response payload log files.
	DebugPayloads        bool     `yaml:"debug_payloads" json:"debug_payloads"`
	PayloadRetentionDays int      `yaml:"payload_retention_days" json:"payload_retention_days"`
	PayloadMaxBytes      ByteSize `yaml:"payload_max_bytes" json:"payload_max_bytes"`
}

type Server struct {
	// Listen is "tcp://host:port" or "unix:///path".
	Listen   string `yaml:"listen" json:"listen"`
	BasePath string `yaml:"base_path" json:"base_path"`
	Auth     Auth   `yaml:"auth" json:"auth"`
}

type Auth struct {
	RequireAdmin bool `yaml:"require_admin" json:"require_admin"`
}

type LLM struct {
	// Provider is a preset id (see llm.Presets) or "custom".
	Provider    string        `yaml:"provider" json:"provider"`
	BaseURL     string        `yaml:"base_url" json:"base_url"`
	APIKey      string        `yaml:"api_key" json:"api_key"`
	Model       string        `yaml:"model" json:"model"`
	Temperature float64       `yaml:"temperature" json:"temperature"`
	Timeout     time.Duration `yaml:"timeout" json:"timeout"`
	// ExtraBody is merged into the chat/completions request body (e.g.
	// enable_thinking: false). Empty = use the provider preset's default.
	ExtraBody map[string]any `yaml:"extra_body" json:"extra_body,omitempty"`
}

// EffectiveExtraBody returns ExtraBody, or the preset default when it is
// empty and Provider names a preset that defines one.
func (l LLM) EffectiveExtraBody() map[string]any {
	if len(l.ExtraBody) > 0 {
		return l.ExtraBody
	}
	if p, ok := llm.PresetByID(l.Provider); ok {
		return p.ExtraBody
	}
	return nil
}

// EffectiveBaseURL returns the base URL actually used: a preset whose base URL
// is not editable always wins over the stored (file/env) value; an editable
// preset falls back to its default when none is stored.
func (l LLM) EffectiveBaseURL() string {
	p, ok := llm.PresetByID(l.Provider)
	if !ok || p.ID == llm.CustomPresetID {
		return l.BaseURL
	}
	if !p.BaseURLEditable || strings.TrimSpace(l.BaseURL) == "" {
		return p.BaseURL
	}
	return l.BaseURL
}

type Tools struct {
	// Dir is the directory holding external binaries (VOX_TOOLS_DIR).
	Dir     string `yaml:"dir" json:"dir"`
	FzqPath string `yaml:"fzq_path" json:"fzq_path"`
	JqPath  string `yaml:"jq_path" json:"jq_path"`
	YqPath  string `yaml:"yq_path" json:"yq_path"`
	Shell   Shell  `yaml:"shell" json:"shell"`
}

type Shell struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	Isolation     string        `yaml:"isolation" json:"isolation"`
	ExposeTo      string        `yaml:"expose_to" json:"expose_to"`
	Timeout       time.Duration `yaml:"timeout" json:"timeout"`
	MaxOutput     ByteSize      `yaml:"max_output" json:"max_output"`
	ReadablePaths []string      `yaml:"readable_paths" json:"readable_paths"`
}

// MediaLibrarySource is the reserved name of the built-in, read-only data
// source over the synced song list; user sources may not use it.
const MediaLibrarySource = "media_library"

// Source is a registered data source (§6.3). The LLM only ever sees Name.
type Source struct {
	Name    string   `yaml:"name" json:"name"`
	File    string   `yaml:"file" json:"file"`
	Format  string   `yaml:"format" json:"format"`
	Records string   `yaml:"records" json:"records"`
	Keys    []string `yaml:"keys" json:"keys"`
	Extract string   `yaml:"extract" json:"extract"`
	Strip   []string `yaml:"strip" json:"strip"`
}

type Media struct {
	Folders         []string      `yaml:"folders" json:"folders"`
	Extensions      []string      `yaml:"extensions" json:"extensions"`
	SyncInterval    time.Duration `yaml:"sync_interval" json:"sync_interval"`
	SyncOnStart     bool          `yaml:"sync_on_start" json:"sync_on_start"`
	MaxFiles        int           `yaml:"max_files" json:"max_files"`
	FilenamePattern string        `yaml:"filename_pattern" json:"filename_pattern"`
	// TagMode: filename_first | tags_first | filename_only (embedded audio tags).
	TagMode string `yaml:"tag_mode" json:"tag_mode"`
}

// UnmarshalYAML lets sync_interval be a bare 0 ("manual only") as well as a
// duration string such as "6h".
func (m *Media) UnmarshalYAML(n *yaml.Node) error {
	type plain Media // no methods: avoids recursion
	cp := *n
	cp.Content = append([]*yaml.Node(nil), n.Content...)
	for i := 0; i+1 < len(cp.Content); i += 2 {
		if cp.Content[i].Value == "sync_interval" {
			if v := cp.Content[i+1]; v.Tag == "!!int" && v.Value == "0" {
				nv := *v
				nv.Tag, nv.Value = "!!str", "0s"
				cp.Content[i+1] = &nv
			}
		}
	}
	return cp.Decode((*plain)(m))
}

type Search struct {
	StripPatterns []string `yaml:"strip_patterns" json:"strip_patterns"`
	MinScore      float64  `yaml:"min_score" json:"min_score"`
	Limit         int      `yaml:"limit" json:"limit"`
	Candidates    int      `yaml:"candidates" json:"candidates"`
}

type Speaker struct {
	Driver string `yaml:"driver" json:"driver"`
	// PublicBaseURL is the address the speaker uses to fetch /audio URLs
	// (e.g. http://192.168.1.10:8081). Empty = auto-detect host IPv4 + port.
	PublicBaseURL string `yaml:"public_base_url" json:"public_base_url"`
	// URLTTL is the lifetime of a signed media URL.
	URLTTL time.Duration `yaml:"url_ttl" json:"url_ttl"`
	// VolumeDefault is the volume used by the speaker "volume" test action when
	// none is given (default 30; 0 = unset). It is never applied automatically.
	VolumeDefault int `yaml:"volume_default" json:"volume_default"`
	// DebugDevice enables the always-available mock device "debug" (调试音箱),
	// independent of the driver.
	DebugDevice bool          `yaml:"debug_device" json:"debug_device"`
	Xiaomi      SpeakerXiaomi `yaml:"xiaomi" json:"xiaomi"`
	Hook        SpeakerHook   `yaml:"hook" json:"hook"`
	Mock        SpeakerMock   `yaml:"mock" json:"mock"`
}

// SpeakerXiaomi holds the Xiaomi driver settings (the account itself lives in
// <data>/mi_token.json, never in the config).
type SpeakerXiaomi struct {
	Devices []SpeakerDevice `yaml:"devices" json:"devices"`
}

// SpeakerDevice is one configured speaker (also used by the mock driver).
type SpeakerDevice struct {
	DeviceID string `yaml:"device_id" json:"device_id"`
	Name     string `yaml:"name" json:"name"`
	Hardware string `yaml:"hardware" json:"hardware"`
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	PlayMode string `yaml:"play_mode" json:"play_mode"` // auto|url|music
	TTSMode  string `yaml:"tts_mode" json:"tts_mode"`   // auto|mina|miot
	MiotTTS  string `yaml:"miot_tts" json:"miot_tts"`   // "siid-aiid", empty = by model
}

// SpeakerHook configures the conversation hook (docs/DESIGN.md §6.7.3).
type SpeakerHook struct {
	Mode     string   `yaml:"mode" json:"mode"` // music|keyword|all|off
	Keywords []string `yaml:"keywords" json:"keywords"`
	// WakeWords are stripped from the start of every ask before matching.
	WakeWords []string `yaml:"wake_words" json:"wake_words"`
	// MusicTriggers are the play verbs of mode music.
	MusicTriggers []string `yaml:"music_triggers" json:"music_triggers"`
	// MusicExclude are phrases that look like triggers but are not music requests.
	MusicExclude []string `yaml:"music_exclude" json:"music_exclude"`
	// DirectPlayScore is the minimum search score (1-100) to skip the LLM.
	DirectPlayScore int           `yaml:"direct_play_score" json:"direct_play_score"`
	PollInterval    time.Duration `yaml:"poll_interval" json:"poll_interval"`
	InterruptWindow time.Duration `yaml:"interrupt_window" json:"interrupt_window"`
}

// SpeakerMock configures fault injection of the mock driver.
type SpeakerMock struct {
	LatencyMS int     `yaml:"latency_ms" json:"latency_ms"`
	FailRate  float64 `yaml:"fail_rate" json:"fail_rate"`
}

// Defaults returns the built-in defaults (before file/env). Tool paths are
// left empty and resolved from Tools.Dir by Load.
func Defaults() *Config {
	return &Config{
		DataDir: DefaultDataDir,
		Server: Server{
			Listen: "tcp://127.0.0.1:8080",
		},
		LLM: LLM{
			Provider:    llm.CustomPresetID,
			BaseURL:     "https://api.openai.com/v1",
			Model:       "gpt-4o-mini",
			Temperature: 0.3,
			Timeout:     60 * time.Second,
		},
		Tools: Tools{
			Dir: DefaultToolsDir,
			Shell: Shell{
				Isolation: "landlock",
				ExposeTo:  "admin_console",
				Timeout:   10 * time.Second,
				MaxOutput: 64 << 10,
			},
		},
		Media: Media{
			Folders:         []string{"/vol1/1000/Music"},
			Extensions:      []string{"mp3", "flac", "m4a", "aac", "wav", "ogg", "ape"},
			SyncInterval:    6 * time.Hour,
			SyncOnStart:     true,
			MaxFiles:        100000,
			FilenamePattern: `(?:(?P<artist>[^/]+?) - )?(?P<title>[^/]+)\.\w+$`,
			TagMode:         "filename_first",
		},
		Search: Search{
			StripPatterns: []string{
				`^(下一首|上一首|暂停|播放音乐)$`,
				`^(小爱同学|小度)?[，, ]*(请|帮我)?(播放|放|来|我想听|我要听|想听|听)(一下|一首|首|点)?`,
				`(的歌|吧|呀)$`,
			},
			MinScore:   65,
			Limit:      5,
			Candidates: 500,
		},
		Speaker: Speaker{Driver: "mock", URLTTL: time.Hour, VolumeDefault: 30, DebugDevice: true,
			Hook: SpeakerHook{Mode: "music", Keywords: []string{"问问"},
				WakeWords:       []string{"小爱同学", "小爱"},
				MusicTriggers:   []string{"播放", "放一首", "放首", "放一下", "放", "来一首", "来首", "来点", "我想听", "想听", "听一下", "听听", "听"},
				MusicExclude:    []string{"放假", "放心", "放松", "听说", "听话", "听见"},
				DirectPlayScore: 85,
				PollInterval:    time.Second, InterruptWindow: 3 * time.Second}},
		Logging: Logging{
			Level:                "info",
			TraceRetentionDays:   30,
			TraceMaxRows:         100000,
			PayloadRetentionDays: 7,
			PayloadMaxBytes:      200 << 20,
		},
	}
}

// Load builds the effective configuration. dataDir may be empty, in which
// case VOX_DATA_DIR and then "/data" are used. If <dataDir>/config.yaml does
// not exist the embedded default is written there (mode 0600).
func Load(dataDir string) (*Config, error) {
	return load(dataDir, os.LookupEnv)
}

func load(dataDir string, lookup func(string) (string, bool)) (*Config, error) {
	if dataDir == "" {
		if v, ok := lookup("VOX_DATA_DIR"); ok && v != "" {
			dataDir = v
		} else {
			dataDir = DefaultDataDir
		}
	}
	cfg := Defaults()
	cfg.DataDir = dataDir

	path := filepath.Join(dataDir, FileName)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return nil, fmt.Errorf("config: create data dir: %w", err)
		}
		if err := os.WriteFile(path, defaultYAML, 0o600); err != nil {
			return nil, fmt.Errorf("config: write default %s: %w", path, err)
		}
		raw = defaultYAML
	case err != nil:
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, errEOF) {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if err := applyEnv(cfg, lookup); err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir
	cfg.resolve(lookup)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(c *Config, lookup func(string) (string, bool)) error {
	var errs []error
	str := func(name string, dst *string) {
		if v, ok := lookup(name); ok && v != "" {
			*dst = v
		}
	}
	boolean := func(name string, dst *bool) {
		if v, ok := lookup(name); ok && v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("config: %s: %w", name, err))
				return
			}
			*dst = b
		}
	}
	dur := func(name string, dst *time.Duration) {
		if v, ok := lookup(name); ok && v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("config: %s: %w", name, err))
				return
			}
			*dst = d
		}
	}
	str("VOX_LISTEN", &c.Server.Listen)
	str("VOX_BASE_PATH", &c.Server.BasePath)
	boolean("VOX_REQUIRE_ADMIN", &c.Server.Auth.RequireAdmin)
	str("VOX_LLM_PROVIDER", &c.LLM.Provider)
	str("VOX_LLM_BASE_URL", &c.LLM.BaseURL)
	str("VOX_LLM_API_KEY", &c.LLM.APIKey)
	str("VOX_LLM_MODEL", &c.LLM.Model)
	dur("VOX_LLM_TIMEOUT", &c.LLM.Timeout)
	if v, ok := lookup("VOX_LLM_TEMPERATURE"); ok && v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: VOX_LLM_TEMPERATURE: %w", err))
		} else {
			c.LLM.Temperature = f
		}
	}
	str("VOX_TOOLS_DIR", &c.Tools.Dir)
	str("VOX_FZQ_PATH", &c.Tools.FzqPath)
	str("VOX_JQ_PATH", &c.Tools.JqPath)
	str("VOX_YQ_PATH", &c.Tools.YqPath)
	boolean("VOX_SHELL_ENABLED", &c.Tools.Shell.Enabled)
	str("VOX_SPEAKER_DRIVER", &c.Speaker.Driver)
	str("VOX_PUBLIC_BASE_URL", &c.Speaker.PublicBaseURL)
	return errors.Join(errs...)
}

// resolve fills derived defaults (tool paths from Tools.Dir).
func (c *Config) resolve(_ func(string) (string, bool)) {
	// Migration: the old keyword-mode default (["请","你","问问"]) becomes music mode.
	if h := &c.Speaker.Hook; h.Mode == "keyword" && len(h.Keywords) == 3 &&
		h.Keywords[0] == "请" && h.Keywords[1] == "你" && h.Keywords[2] == "问问" {
		h.Mode, h.Keywords = "music", []string{"问问"}
	}
	if c.LLM.Provider == "" {
		c.LLM.Provider = llm.CustomPresetID
	}
	if _, ok := llm.PresetByID(c.LLM.Provider); ok {
		c.LLM.BaseURL = c.LLM.EffectiveBaseURL()
	}
	if c.Tools.Dir == "" {
		c.Tools.Dir = DefaultToolsDir
	}
	if c.Tools.FzqPath == "" {
		c.Tools.FzqPath = filepath.Join(c.Tools.Dir, "fzq")
	}
	if c.Tools.JqPath == "" {
		c.Tools.JqPath = filepath.Join(c.Tools.Dir, "jq")
	}
	if c.Tools.YqPath == "" {
		c.Tools.YqPath = filepath.Join(c.Tools.Dir, "yq")
	}
}

// Validate checks structural invariants.
func (c *Config) Validate() error {
	if _, ok := llm.PresetByID(c.LLM.Provider); !ok {
		return fmt.Errorf("config: llm.provider %q is not a known provider", c.LLM.Provider)
	}
	if _, _, err := ParseListen(c.Server.Listen); err != nil {
		return err
	}
	if err := mediaurl.ValidateBase(c.Speaker.PublicBaseURL); err != nil {
		return fmt.Errorf("config: speaker.public_base_url: %w", err)
	}
	if c.Speaker.URLTTL <= 0 {
		return errors.New("config: speaker.url_ttl must be > 0")
	}
	if err := c.Speaker.validate(); err != nil {
		return err
	}
	switch c.Tools.Shell.ExposeTo {
	case "admin_console", "all":
	default:
		return fmt.Errorf("config: tools.shell.expose_to must be admin_console|all, got %q", c.Tools.Shell.ExposeTo)
	}
	if c.Tools.Shell.Isolation != "landlock" {
		return fmt.Errorf("config: tools.shell.isolation %q unsupported (only landlock)", c.Tools.Shell.Isolation)
	}
	seen := map[string]bool{}
	for i, s := range c.Sources {
		if s.Name == "" || s.File == "" {
			return fmt.Errorf("config: sources[%d]: name and file are required", i)
		}
		if s.Name == MediaLibrarySource {
			return fmt.Errorf("config: sources[%d]: name %q is reserved for the built-in media library source", i, s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("config: duplicate source name %q", s.Name)
		}
		seen[s.Name] = true
	}
	if c.Media.SyncInterval < 0 {
		return errors.New("config: media.sync_interval must be >= 0")
	}
	switch c.Media.TagMode {
	case "filename_first", "tags_first", "filename_only":
	default:
		return fmt.Errorf("config: media.tag_mode must be filename_first|tags_first|filename_only, got %q", c.Media.TagMode)
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: logging.level must be debug|info|warn|error, got %q", c.Logging.Level)
	}
	return nil
}

// ParseListen splits a listen URL into network ("tcp"/"unix") and address.
func ParseListen(s string) (network, addr string, err error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("config: server.listen %q: %w", s, err)
	}
	switch u.Scheme {
	case "tcp":
		if u.Host == "" {
			return "", "", fmt.Errorf("config: server.listen %q: missing host:port", s)
		}
		return "tcp", u.Host, nil
	case "unix":
		if u.Path == "" {
			return "", "", fmt.Errorf("config: server.listen %q: missing socket path", s)
		}
		return "unix", u.Path, nil
	}
	return "", "", fmt.Errorf("config: server.listen %q: scheme must be tcp or unix", s)
}

// SourcesDir is where relative source files live.
func (c *Config) SourcesDir() string { return filepath.Join(c.DataDir, "sources") }

// SourcePath resolves a source's file (relative -> <dataDir>/sources).
func (c *Config) SourcePath(s Source) string {
	if filepath.IsAbs(s.File) {
		return s.File
	}
	return filepath.Join(c.SourcesDir(), s.File)
}

// DBPath is the SQLite database location.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "app.db") }

// Source returns a registered source by name.
func (c *Config) Source(name string) (Source, bool) {
	for _, s := range c.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// Clone returns a deep copy (slices, maps and nested structs are not shared).
func (c *Config) Clone() *Config {
	cp := *c
	cp.LLM.ExtraBody = cloneMap(c.LLM.ExtraBody)
	cp.Tools.Shell.ReadablePaths = append([]string(nil), c.Tools.Shell.ReadablePaths...)
	cp.Media.Folders = append([]string(nil), c.Media.Folders...)
	cp.Media.Extensions = append([]string(nil), c.Media.Extensions...)
	cp.Search.StripPatterns = append([]string(nil), c.Search.StripPatterns...)
	cp.Speaker.Xiaomi.Devices = append([]SpeakerDevice(nil), c.Speaker.Xiaomi.Devices...)
	cp.Speaker.Hook.Keywords = append([]string(nil), c.Speaker.Hook.Keywords...)
	cp.Speaker.Hook.WakeWords = append([]string(nil), c.Speaker.Hook.WakeWords...)
	cp.Speaker.Hook.MusicTriggers = append([]string(nil), c.Speaker.Hook.MusicTriggers...)
	cp.Speaker.Hook.MusicExclude = append([]string(nil), c.Speaker.Hook.MusicExclude...)
	cp.Sources = make([]Source, len(c.Sources))
	for i, s := range c.Sources {
		s.Keys = append([]string(nil), s.Keys...)
		s.Strip = append([]string(nil), s.Strip...)
		cp.Sources[i] = s
	}
	return &cp
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneValue(x[i])
		}
		return out
	}
	return v
}

// Redacted returns a deep copy safe to show in UIs/logs: api_key is masked,
// only the last 4 characters remain visible.
func (c *Config) Redacted() *Config {
	cp := c.Clone()
	cp.LLM.APIKey = maskSecret(c.LLM.APIKey)
	return cp
}

// MaskSecret masks a secret for display: only the last 4 characters remain.
func MaskSecret(s string) string { return maskSecret(s) }

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 4 {
		return strings.Repeat("*", len(r))
	}
	return "****" + string(r[len(r)-4:])
}

var miotTTSRe = regexp.MustCompile(`^[0-9]{1,3}-[0-9]{1,3}$`)

func (s Speaker) validate() error {
	switch s.Driver {
	case "mock", "log", "xiaomi":
	default:
		return fmt.Errorf("config: speaker.driver must be mock|log|xiaomi, got %q", s.Driver)
	}
	if s.VolumeDefault < 0 || s.VolumeDefault > 100 {
		return errors.New("config: speaker.volume_default must be 0-100")
	}
	switch s.Hook.Mode {
	case "music", "keyword", "all", "off":
	default:
		return fmt.Errorf("config: speaker.hook.mode must be music|keyword|all|off, got %q", s.Hook.Mode)
	}
	if s.Hook.DirectPlayScore < 1 || s.Hook.DirectPlayScore > 100 {
		return errors.New("config: speaker.hook.direct_play_score must be 1-100")
	}
	if s.Hook.PollInterval < 500*time.Millisecond || s.Hook.PollInterval > 5*time.Second {
		return errors.New("config: speaker.hook.poll_interval must be between 500ms and 5s")
	}
	if s.Hook.InterruptWindow < 0 || s.Hook.InterruptWindow > 30*time.Second {
		return errors.New("config: speaker.hook.interrupt_window must be between 0 and 30s")
	}
	if s.Mock.LatencyMS < 0 || s.Mock.LatencyMS > 60000 {
		return errors.New("config: speaker.mock.latency_ms must be 0-60000")
	}
	if s.Mock.FailRate < 0 || s.Mock.FailRate > 1 {
		return errors.New("config: speaker.mock.fail_rate must be 0-1")
	}
	seen := map[string]bool{}
	for i, d := range s.Xiaomi.Devices {
		if strings.TrimSpace(d.DeviceID) == "" {
			return fmt.Errorf("config: speaker.xiaomi.devices[%d]: device_id is required", i)
		}
		if seen[d.DeviceID] {
			return fmt.Errorf("config: speaker.xiaomi.devices: duplicate device_id %q", d.DeviceID)
		}
		seen[d.DeviceID] = true
		switch d.PlayMode {
		case "", "auto", "url", "music":
		default:
			return fmt.Errorf("config: speaker.xiaomi.devices[%d].play_mode must be auto|url|music", i)
		}
		switch d.TTSMode {
		case "", "auto", "mina", "miot":
		default:
			return fmt.Errorf("config: speaker.xiaomi.devices[%d].tts_mode must be auto|mina|miot", i)
		}
		if d.MiotTTS != "" && !miotTTSRe.MatchString(d.MiotTTS) {
			return fmt.Errorf("config: speaker.xiaomi.devices[%d].miot_tts must look like 5-1", i)
		}
	}
	return nil
}
