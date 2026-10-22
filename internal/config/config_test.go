package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		file  string // "" => none (default file is created)
		env   map[string]string
		check func(t *testing.T, c *Config)
	}{
		{
			name: "defaults only (embedded default file)",
			check: func(t *testing.T, c *Config) {
				if c.LLM.Model != "gpt-4o-mini" || c.Search.MinScore != 65 || c.Search.Limit != 5 || c.Search.Candidates != 500 {
					t.Errorf("unexpected defaults: %+v %+v", c.LLM, c.Search)
				}
				if len(c.Search.StripPatterns) != 3 {
					t.Errorf("strip patterns: %v", c.Search.StripPatterns)
				}
				if c.Tools.FzqPath != "/usr/local/bin/fzq" || c.Tools.JqPath != "/usr/local/bin/jq" {
					t.Errorf("tool paths: %+v", c.Tools)
				}
				if c.Agent.Temperature != nil {
					t.Errorf("agent temperature default: %v", *c.Agent.Temperature)
				}
				if c.Media.SyncInterval != 6*time.Hour || !c.Media.SyncOnStart || c.Speaker.Driver != "mock" {
					t.Errorf("media/speaker: %+v %+v", c.Media, c.Speaker)
				}
			},
		},
		{
			name: "file overrides defaults",
			file: "llm:\n  model: file-model\n  temperature: 0\n  timeout: 5s\n  extra_body:\n    enable_thinking: false\n    nested: {a: 1}\ntools:\n  shell:\n    max_output: 1MiB\n    enabled: true\nmedia:\n  sync_interval: 0\nsources:\n  - {name: contacts, file: c.json, format: json, keys: [name]}\n",
			check: func(t *testing.T, c *Config) {
				if c.LLM.Model != "file-model" || c.LLM.Timeout != 5*time.Second {
					t.Errorf("llm: %+v", c.LLM)
				}
				if eb := c.LLM.EffectiveExtraBody(); eb["enable_thinking"] != false || eb["nested"] == nil {
					t.Errorf("extra_body: %+v", eb)
				}
				if c.Agent.Temperature == nil || *c.Agent.Temperature != 0 {
					t.Errorf("legacy llm.temperature not migrated: %v", c.Agent.Temperature)
				}
				if c.Media.SyncInterval != 0 {
					t.Errorf("sync interval: %v", c.Media.SyncInterval)
				}
			},
		},
		{
			name: "env overrides file",
			file: "llm:\n  model: file-model\n  api_key: filekey1234\n",
			env: map[string]string{
				"VOX_LLM_MODEL": "env-model", "VOX_LLM_API_KEY": "envkey-abcd", "VOX_LLM_BASE_URL": "http://x/v1",
				"VOX_TOOLS_DIR": "/opt/tools", "VOX_REQUIRE_ADMIN": "true",
			},
			check: func(t *testing.T, c *Config) {
				if c.LLM.Model != "env-model" || c.LLM.APIKey != "envkey-abcd" || c.LLM.BaseURL != "http://x/v1" {
					t.Errorf("llm: %+v", c.LLM)
				}
				if c.Tools.FzqPath != "/opt/tools/fzq" || c.Tools.YqPath != "/opt/tools/yq" {
					t.Errorf("tools: %+v", c.Tools)
				}
				if !c.Server.Auth.RequireAdmin {
					t.Error("require_admin not set")
				}
			},
		},
		{
			name: "explicit tool path beats tools dir",
			file: "tools:\n  fzq_path: /custom/fzq\n",
			env:  map[string]string{"VOX_TOOLS_DIR": "/opt/tools"},
			check: func(t *testing.T, c *Config) {
				if c.Tools.FzqPath != "/custom/fzq" || c.Tools.JqPath != "/opt/tools/jq" {
					t.Errorf("tools: %+v", c.Tools)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			c, err := load(dir, envMap(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, c)
		})
	}
}

func TestDataDirFromEnv(t *testing.T) {
	dir := t.TempDir()
	c, err := load("", envMap(map[string]string{"VOX_DATA_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != dir || c.DBPath() != filepath.Join(dir, "app.db") {
		t.Errorf("DataDir=%q", c.DataDir)
	}
}

func TestDefaultFileCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	if _, err := load(dir, envMap(nil)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, FileName)
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(DefaultYAML()) {
		t.Error("content differs from embedded default")
	}
	// Existing file is not overwritten.
	if err := os.WriteFile(p, []byte("speaker:\n  driver: xiaomi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := load(dir, envMap(nil))
	if err != nil || c.Speaker.Driver != "xiaomi" {
		t.Fatalf("err=%v c=%+v", err, c)
	}
}

func TestInvalid(t *testing.T) {
	tests := map[string]string{
		"unknown key": "bogus: 1\n",
		"bad listen":  "server:\n  listen: http://x\n",
	}
	for name, y := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, FileName), []byte(y), 0o600)
			if _, err := load(dir, envMap(nil)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := load(t.TempDir(), envMap(map[string]string{"VOX_REQUIRE_ADMIN": "maybe"})); err == nil {
		t.Error("expected bool env error")
	}
}

func TestRedacted(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"abc", "***"},
		{"sk-1234567890wxyz", "****wxyz"},
	}
	for _, tc := range tests {
		c := Defaults()
		c.LLM.APIKey = tc.in
		r := c.Redacted()
		if r.LLM.APIKey != tc.want {
			t.Errorf("%q -> %q want %q", tc.in, r.LLM.APIKey, tc.want)
		}
		if c.LLM.APIKey != tc.in {
			t.Error("original mutated")
		}
		r.Media.Folders[0] = "changed"
		if c.Media.Folders[0] == "changed" {
			t.Error("not a deep copy")
		}
	}
}

func TestParseByteSize(t *testing.T) {
	for in, want := range map[string]ByteSize{"64KiB": 64 << 10, "1MiB": 1 << 20, "512": 512, "2kb": 2 << 10, "10B": 10} {
		got, err := ParseByteSize(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if ByteSize(64<<10).String() != "64KiB" {
		t.Error("String")
	}
}

func TestParseListen(t *testing.T) {
	n, a, err := ParseListen("unix:///data/app.sock")
	if err != nil || n != "unix" || a != "/data/app.sock" {
		t.Errorf("%s %s %v", n, a, err)
	}
	n, a, err = ParseListen("tcp://127.0.0.1:8080")
	if err != nil || n != "tcp" || a != "127.0.0.1:8080" {
		t.Errorf("%s %s %v", n, a, err)
	}
}

func TestSplitValidateListen(t *testing.T) {
	items, err := ValidateListen("unix:///sock/app.sock, tcp://0.0.0.0:8080")
	if err != nil || len(items) != 2 || items[1] != "tcp://0.0.0.0:8080" {
		t.Fatalf("%v %v", items, err)
	}
	if got := PrimaryListen("unix:///a.sock,tcp://0.0.0.0:1"); got != "tcp://0.0.0.0:1" {
		t.Errorf("primary %q", got)
	}
	if got := PrimaryListen("unix:///a.sock"); got != "unix:///a.sock" {
		t.Errorf("primary %q", got)
	}
	for _, bad := range []string{"", " , ", "tcp://x:1,http://y", "unix:///a.sock,"} {
		if _, err := ValidateListen(bad); err == nil && bad != "unix:///a.sock," {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestLLMProvider(t *testing.T) {
	load1 := func(file string, env map[string]string) (*Config, error) {
		dir := t.TempDir()
		if file != "" {
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return load(dir, envMap(env))
	}
	c, err := load1("", nil)
	if err != nil || c.LLM.Provider != "custom" || c.LLM.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("default: %+v %v", c.LLM, err)
	}
	// legacy file without provider stays custom with its base_url
	c, err = load1("llm:\n  base_url: http://x/v1\n", nil)
	if err != nil || c.LLM.Provider != "custom" || c.LLM.EffectiveBaseURL() != "http://x/v1" {
		t.Fatalf("legacy: %+v %v", c.LLM, err)
	}
	// non-editable preset ignores file value
	c, err = load1("llm:\n  provider: deepseek\n  base_url: http://evil/v1\n", nil)
	if err != nil || c.LLM.EffectiveBaseURL() != "https://api.deepseek.com/v1" || c.LLM.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("deepseek: %+v %v", c.LLM, err)
	}
	// editable preset keeps file value, defaults when empty
	c, err = load1("llm:\n  provider: ollama\n  base_url: http://nas:11434/v1\n", nil)
	if err != nil || c.LLM.EffectiveBaseURL() != "http://nas:11434/v1" {
		t.Fatalf("ollama: %+v %v", c.LLM, err)
	}
	c, err = load1("llm:\n  provider: ollama\n  base_url: \"\"\n", nil)
	if err != nil || c.LLM.EffectiveBaseURL() != "http://127.0.0.1:11434/v1" {
		t.Fatalf("ollama default: %+v %v", c.LLM, err)
	}
	// env provider
	c, err = load1("", map[string]string{"VOX_LLM_PROVIDER": "openai"})
	if err != nil || c.LLM.Provider != "openai" || c.LLM.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("env: %+v %v", c.LLM, err)
	}
	if _, err = load1("llm:\n  provider: bogus\n", nil); err == nil {
		t.Error("unknown provider accepted")
	}
}

func TestHookDefaultsAndMigration(t *testing.T) {
	c := Defaults()
	h := c.Speaker.Hook
	if h.Mode != "music" || len(h.Keywords) != 1 || h.Keywords[0] != "问问" || h.WakeWords[0] != "小爱同学" || h.DirectPlayScore != 85 || len(h.MusicTriggers) != 13 || len(h.MusicExclude) != 6 {
		t.Fatalf("%+v", h)
	}
	dir := t.TempDir()
	old := "speaker:\n  driver: mock\n  hook:\n    mode: keyword\n    keywords: [\"请\", \"你\", \"问问\"]\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Speaker.Hook.Mode != "music" || len(got.Speaker.Hook.Keywords) != 1 || got.Speaker.Hook.Keywords[0] != "问问" {
		t.Fatalf("not migrated: %+v", got.Speaker.Hook)
	}
	// customised keyword configs are left alone
	custom := "speaker:\n  hook:\n    mode: keyword\n    keywords: [\"请\", \"问问\"]\n"
	_ = os.WriteFile(filepath.Join(dir, FileName), []byte(custom), 0o600)
	got, err = Load(dir)
	if err != nil || got.Speaker.Hook.Mode != "keyword" || len(got.Speaker.Hook.Keywords) != 2 {
		t.Fatalf("%v %+v", err, got.Speaker.Hook)
	}
}

func TestSpeakerDriverLogMigratesToMock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("speaker:\n  driver: log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || c.Speaker.Driver != "mock" {
		t.Fatalf("%v %q", err, c.Speaker.Driver)
	}
}

func TestMCPConfigValidate(t *testing.T) {
	ok := MCPServer{Name: "a", Transport: MCPHTTP, URL: "http://x/mcp"}
	bad := []MCPServer{
		{Name: "a", Transport: "ws", URL: "http://x"},
		{Name: "a", Transport: MCPHTTP},
		{Name: "a", Transport: MCPStdio},
		{Name: "1a", Transport: MCPStdio, Command: "x"},
	}
	if err := validateMCP([]MCPServer{ok, {Name: "b", Transport: MCPStdio, Command: "x"}}); err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		if validateMCP([]MCPServer{b}) == nil {
			t.Errorf("accepted %+v", b)
		}
	}
	if validateMCP([]MCPServer{ok, ok}) == nil {
		t.Error("duplicate accepted")
	}
	c := Defaults()
	c.MCP.Servers = []MCPServer{{Name: "a", Transport: MCPHTTP, URL: "http://x", Headers: map[string]string{"K": "secretvalue"}}}
	if r := c.Redacted(); r.MCP.Servers[0].Headers["K"] != "****alue" || c.MCP.Servers[0].Headers["K"] != "secretvalue" {
		t.Errorf("redaction: %+v", r.MCP.Servers[0].Headers)
	}
}

func TestEnvLockedRecorded(t *testing.T) {
	c, err := load(t.TempDir(), envMap(map[string]string{"VOX_LLM_MODEL": "m", "VOX_LISTEN": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if c.EnvLocked["llm.model"] != "VOX_LLM_MODEL" || len(c.EnvLocked) != 1 {
		t.Fatalf("%v", c.EnvLocked)
	}
	if c.Clone().EnvLocked["llm.model"] != "VOX_LLM_MODEL" {
		t.Fatal("clone lost EnvLocked")
	}
}
