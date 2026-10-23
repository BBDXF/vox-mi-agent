// Package speaker defines the speaker driver abstraction (mock |
// xiaomi), the Manager that adapts it to the audio tools, and the Hook that
// lets the agent take over the speaker's conversations (docs/DESIGN.md §6.7).
package speaker

import (
	"context"
	"errors"
	"time"
)

// Play status values (MiNA player_get_play_status).
const (
	StatusIdle    = 0
	StatusPlaying = 1
	StatusPaused  = 2
	StatusStopped = 3
)

// Device is one speaker with its per-device overrides.
type Device struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Hardware string `json:"hardware"`
	Enabled  bool   `json:"enabled"`
	PlayMode string `json:"play_mode"` // auto|url|music
	TTSMode  string `json:"tts_mode"`  // auto|mina|miot
	MiotTTS  string `json:"miot_tts"`  // "siid-aiid" or empty
	Online   *bool  `json:"online,omitempty"`
}

// Ask is a question the user spoke to the speaker.
type Ask struct {
	DeviceID string    `json:"device_id"`
	Query    string    `json:"query"`
	Time     time.Time `json:"time"`
	Answer   string    `json:"answer,omitempty"` // the speaker's native answer, if any
	// Native is set by the mock when it simulates a native assistant reply.
	Native bool `json:"native,omitempty"`
	// TextTriggered: typed via the web UI, not heard by the device (no interrupt).
	TextTriggered bool `json:"text_triggered,omitempty"`
}

// Status is the player state.
type Status struct {
	Status int `json:"status"` // StatusIdle..StatusStopped
	Volume int `json:"volume,omitempty"`
}

// Playing reports whether audio is currently playing.
func (s Status) Playing() bool { return s.Status == StatusPlaying }

// Driver is implemented by mock and xiaomi (method names follow MiNAService).
type Driver interface {
	Name() string
	DeviceList(ctx context.Context) ([]Device, error)
	// GetLatestAsk returns the newest ask strictly after since, or nil.
	GetLatestAsk(ctx context.Context, d Device, since time.Time) (*Ask, error)
	TextToSpeech(ctx context.Context, d Device, text string) error
	PlayByURL(ctx context.Context, d Device, url string) error
	PlayerSetVolume(ctx context.Context, d Device, volume int) error
	PlayerPause(ctx context.Context, d Device) error
	PlayerPlay(ctx context.Context, d Device) error
	PlayerStop(ctx context.Context, d Device) error
	PlayerGetStatus(ctx context.Context, d Device) (Status, error)
}

// ErrNoDevice means no speaker is configured / enabled.
var ErrNoDevice = errors.New("no speaker device is configured (enable one in speaker settings)")

// ErrNotImplemented is returned for unsupported driver features.
var ErrNotImplemented = errors.New("not implemented")
