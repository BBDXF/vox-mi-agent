package xiaomibridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultConfigPath is on /data, the speaker's persistent partition.
const DefaultConfigPath = "/data/xiaomi-bridge/config.json"

// Built-in credentials, used until changed through PUT /v1/config.
const (
	DefaultUsername = "admin"
	DefaultPassword = "xiaomi-bridge"
)

// Settings is the persisted, API-editable configuration.
type Settings struct {
	Device string       `json:"device"`
	Auth   AuthSettings `json:"auth"`
	Push   PushSettings `json:"push"`
}

// AuthSettings are the HTTP Basic credentials of the API.
type AuthSettings struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// PushSettings configure the outgoing webhook.
type PushSettings struct {
	URL     string `json:"url"`     // "" disables push
	Token   string `json:"token"`   // sent as "Authorization: Bearer <token>"
	Partial bool   `json:"partial"` // also push non-final results
}

// Patch is a partial update; nil fields are left unchanged.
type Patch struct {
	Device *string `json:"device,omitempty"`
	Auth   *struct {
		Username *string `json:"username,omitempty"`
		Password *string `json:"password,omitempty"`
	} `json:"auth,omitempty"`
	Push *struct {
		URL     *string `json:"url,omitempty"`
		Token   *string `json:"token,omitempty"`
		Partial *bool   `json:"partial,omitempty"`
	} `json:"push,omitempty"`
}

// ConfigView is Settings without secrets.
type ConfigView struct {
	Path   string `json:"path"`
	Device string `json:"device"`
	Auth   struct {
		Username           string `json:"username"`
		DefaultCredentials bool   `json:"default_credentials"`
	} `json:"auth"`
	Push struct {
		URL      string `json:"url"`
		TokenSet bool   `json:"token_set"`
		Partial  bool   `json:"partial"`
	} `json:"push"`
}

// Store holds the settings and persists every change to its file.
type Store struct {
	path string
	mu   sync.RWMutex
	s    Settings
}

// OpenStore loads path; a missing file yields the defaults (written on the
// first update). An unreadable or invalid file is an error, never reset.
func OpenStore(path, defaultDevice string) (*Store, error) {
	st := &Store{path: path, s: Settings{Device: defaultDevice, Auth: AuthSettings{Username: DefaultUsername, Password: DefaultPassword}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if s.Device == "" {
		s.Device = defaultDevice
	}
	if s.Auth.Username == "" || s.Auth.Password == "" {
		s.Auth = st.s.Auth
	}
	if err := validate(s); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	st.s = s
	return st, nil
}

// Get returns a copy of the current settings.
func (st *Store) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.s
}

// View returns the settings without secrets.
func (st *Store) View() ConfigView {
	s := st.Get()
	var v ConfigView
	v.Path = st.path
	v.Device = s.Device
	v.Auth.Username = s.Auth.Username
	v.Auth.DefaultCredentials = s.Auth.Username == DefaultUsername && s.Auth.Password == DefaultPassword
	v.Push.URL = s.Push.URL
	v.Push.TokenSet = s.Push.Token != ""
	v.Push.Partial = s.Push.Partial
	return v
}

// Update validates and persists p; on error nothing changes.
func (st *Store) Update(p Patch) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	s := st.s
	if p.Device != nil {
		s.Device = strings.TrimSpace(*p.Device)
	}
	if a := p.Auth; a != nil {
		if a.Username != nil {
			s.Auth.Username = *a.Username
		}
		if a.Password != nil {
			s.Auth.Password = *a.Password
		}
	}
	if q := p.Push; q != nil {
		if q.URL != nil {
			s.Push.URL = strings.TrimSpace(*q.URL)
		}
		if q.Token != nil {
			s.Push.Token = *q.Token
		}
		if q.Partial != nil {
			s.Push.Partial = *q.Partial
		}
	}
	if err := validate(s); err != nil {
		return err
	}
	if err := writeFileAtomic(st.path, s); err != nil {
		return err
	}
	st.s = s
	return nil
}

// DecodePatch parses a patch, rejecting unknown fields (typos would otherwise
// be silently ignored).
func DecodePatch(b []byte) (Patch, error) {
	var p Patch
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Patch{}, err
	}
	return p, nil
}

func validate(s Settings) error {
	switch {
	case s.Device == "":
		return errors.New("device must not be empty")
	case s.Auth.Username == "" || strings.Contains(s.Auth.Username, ":"):
		return errors.New("auth.username must be non-empty and contain no ':'")
	case len(s.Auth.Password) < 6:
		return errors.New("auth.password must be at least 6 characters")
	case s.Push.URL != "" && !httpURL(s.Push.URL):
		return errors.New("push.url must be an http(s) URL or empty")
	}
	return nil
}

func writeFileAtomic(path string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
