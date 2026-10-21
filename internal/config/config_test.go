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
				if c.Tools.Shell.MaxOutput != 64<<10 || c.Tools.Shell.Timeout != 10*time.Second || c.Tools.Shell.Enabled {
					t.Errorf("shell: %+v", c.Tools.Shell)
				}
				if c.Media.SyncInterval != 6*time.Hour || !c.Media.SyncOnStart || c.Speaker.Driver != "mock" {
					t.Errorf("media/speaker: %+v %+v", c.Media, c.Speaker)
				}
			},
		},
		{
			name: "file overrides defaults",
			file: "llm:\n  model: file-model\n  temperature: 0\n  timeout: 5s\ntools:\n  shell:\n    max_output: 1MiB\nmedia:\n  sync_interval: 0\nsources:\n  - {name: contacts, file: c.json, format: json, keys: [name]}\n",
			check: func(t *testing.T, c *Config) {
				if c.LLM.Model != "file-model" || c.LLM.Temperature != 0 || c.LLM.Timeout != 5*time.Second {
					t.Errorf("llm: %+v", c.LLM)
				}
				if c.Tools.Shell.MaxOutput != 1<<20 || c.Tools.Shell.Isolation != "landlock" {
					t.Errorf("shell: %+v", c.Tools.Shell)
				}
				if c.Media.SyncInterval != 0 {
					t.Errorf("sync interval: %v", c.Media.SyncInterval)
				}
				if len(c.Sources) != 1 || c.SourcePath(c.Sources[0]) != filepath.Join(c.DataDir, "sources", "c.json") {
					t.Errorf("sources: %+v", c.Sources)
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
		"unknown key":    "bogus: 1\n",
		"bad listen":     "server:\n  listen: http://x\n",
		"dup source":     "sources:\n  - {name: a, file: a}\n  - {name: a, file: b}\n",
		"bad expose":     "tools:\n  shell:\n    expose_to: nobody\n",
		"bad bytesize":   "tools:\n  shell:\n    max_output: lots\n",
		"source no file": "sources:\n  - {name: a}\n",
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
		c.Sources = []Source{{Name: "a", File: "a", Keys: []string{"k"}}}
		r := c.Redacted()
		if r.LLM.APIKey != tc.want {
			t.Errorf("%q -> %q want %q", tc.in, r.LLM.APIKey, tc.want)
		}
		if c.LLM.APIKey != tc.in {
			t.Error("original mutated")
		}
		r.Sources[0].Keys[0] = "changed"
		r.Media.Folders[0] = "changed"
		if c.Sources[0].Keys[0] != "k" || c.Media.Folders[0] == "changed" {
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
