package speaker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/andy/vox-mi-agent/internal/tools/audio"
)

// Manager routes speaker operations to the active driver. It implements
// audio.Player (PlayByURL / volume on the default device).
type Manager struct {
	mu      sync.RWMutex
	drv     Driver
	devices []Device // configured devices
	volume  int
	log     *slog.Logger
}

var _ audio.Player = (*Manager)(nil)

// NewManager creates a Manager with a log driver until Set is called.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{drv: NewLog(log), log: log}
}

// Set swaps the driver and device list (hot reload).
func (m *Manager) Set(drv Driver, devices []Device, volumeDefault int) {
	m.mu.Lock()
	m.drv, m.volume = drv, volumeDefault
	m.devices = append([]Device(nil), devices...)
	m.mu.Unlock()
}

// Driver returns the active driver.
func (m *Manager) Driver() Driver { m.mu.RLock(); defer m.mu.RUnlock(); return m.drv }

// VolumeDefault returns speaker.volume_default.
func (m *Manager) VolumeDefault() int { m.mu.RLock(); defer m.mu.RUnlock(); return m.volume }

// Configured returns the configured devices (enabled or not).
func (m *Manager) Configured() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Device(nil), m.devices...)
}

// Devices returns the effective enabled devices: the configured enabled ones;
// with the mock / log driver and none configured, the driver's built-in ones.
func (m *Manager) Devices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Device
	for _, d := range m.devices {
		if d.Enabled {
			out = append(out, d)
		}
	}
	if len(out) == 0 && len(m.devices) == 0 {
		if s, ok := m.drv.(interface{ StaticDevices() []Device }); ok {
			out = s.StaticDevices()
		}
	}
	return out
}

// Resolve finds a device by id ("" = the first enabled device).
func (m *Manager) Resolve(id string) (Device, error) {
	if id == "" {
		l := m.Devices()
		if len(l) == 0 {
			return Device{}, ErrNoDevice
		}
		return l[0], nil
	}
	for _, d := range m.Configured() {
		if d.DeviceID == id {
			return d, nil
		}
	}
	for _, d := range m.Devices() {
		if d.DeviceID == id {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("unknown speaker device %q", id)
}

// PlayQueue holds the play request of one hook turn; it is started only
// after the turn's last sentence has been spoken.
type PlayQueue struct {
	mu  sync.Mutex
	req *audio.PlayRequest
}

func (q *PlayQueue) set(r audio.PlayRequest) { q.mu.Lock(); q.req = &r; q.mu.Unlock() }

// Take returns and clears the pending request (the last one wins).
func (q *PlayQueue) Take() *audio.PlayRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	r := q.req
	q.req = nil
	return r
}

type playQueueKey struct{}

// WithPlayQueue marks ctx as belonging to a hook turn: Manager.Play defers.
func WithPlayQueue(ctx context.Context, q *PlayQueue) context.Context {
	return context.WithValue(ctx, playQueueKey{}, q)
}

// Play implements audio.Player. Inside a hook turn it only records the
// request and returns audio.ErrDeferred.
func (m *Manager) Play(ctx context.Context, r audio.PlayRequest) error {
	if q, ok := ctx.Value(playQueueKey{}).(*PlayQueue); ok {
		q.set(r)
		return audio.ErrDeferred
	}
	d, err := m.Resolve("")
	if err != nil {
		return err
	}
	return m.Driver().PlayByURL(ctx, d, r.URL)
}

// SetVolume implements audio.Player.
func (m *Manager) SetVolume(ctx context.Context, v int) error {
	d, err := m.Resolve("")
	if err != nil {
		return err
	}
	return m.Driver().PlayerSetVolume(ctx, d, v)
}

// Speak sends text to the default device.
func (m *Manager) Speak(ctx context.Context, text string) error {
	d, err := m.Resolve("")
	if err != nil {
		return err
	}
	return m.Driver().TextToSpeech(ctx, d, text)
}

// Recorder is implemented by drivers that keep an action log (mock).
type Recorder interface{ Record(MockEvent) }

// NoteReply records an agent reply in the driver's action log, if it has one.
func (m *Manager) NoteReply(deviceID, text string) {
	if r, ok := m.Driver().(Recorder); ok {
		r.Record(MockEvent{Device: deviceID, Kind: EvReply, Text: text})
	}
}
