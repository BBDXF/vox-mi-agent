package speaker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/tools/audio"
)

// Manager routes speaker operations to the active driver. It implements
// audio.Player (PlayByURL / volume on the default device).
type Manager struct {
	mu      sync.RWMutex
	drv     Driver // what callers use (inner, wrapped to log its actions)
	inner   Driver
	events  *EventLog
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
	m := &Manager{log: log, events: NewEventLog()}
	m.setDriverLocked(NewLog(log))
	return m
}

func (m *Manager) setDriverLocked(d Driver) {
	if d == m.inner && m.drv != nil {
		return
	}
	m.inner = d
	if _, ok := d.(selfRecorder); ok {
		if mk, ok := d.(interface{ UseLog(*EventLog) }); ok {
			mk.UseLog(m.events)
		}
		m.drv = d
		return
	}
	m.drv = &recordingDriver{Driver: d, log: m.events}
}

// Log returns the shared event log (all drivers).
func (m *Manager) Log() *EventLog { return m.events }

// Set swaps the driver and device list (hot reload).
func (m *Manager) Set(drv Driver, devices []Device, volumeDefault int) {
	m.mu.Lock()
	m.setDriverLocked(drv)
	m.volume = volumeDefault
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
		if s, ok := m.inner.(interface{ StaticDevices() []Device }); ok {
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

// PlayMeta is optional track metadata passed to drivers through the context
// (the mock driver shows it in its event log).
type PlayMeta struct{ Title, Artist string }

type playMetaKey struct{}

// WithPlayMeta attaches track metadata to ctx for Driver.PlayByURL.
func WithPlayMeta(ctx context.Context, title, artist string) context.Context {
	return context.WithValue(ctx, playMetaKey{}, PlayMeta{Title: title, Artist: artist})
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
	return m.Driver().PlayByURL(WithPlayMeta(ctx, r.Title, r.Artist), d, r.URL)
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

// NoteReply records an agent reply in the event log.
func (m *Manager) NoteReply(deviceID, text, turnID string) {
	m.events.Record(MockEvent{Device: deviceID, Kind: EvReply, Text: text, TurnID: turnID})
}

// NoteAsk records the hook's decision about an ask (lag = detection lag).
func (m *Manager) NoteAsk(deviceID, query string, lag time.Duration, handled bool, route, turnID string) {
	ms := max(lag.Milliseconds(), 0)
	m.events.Record(MockEvent{Device: deviceID, Kind: EvAskDetected, Text: query, LatencyMS: &ms, Handled: &handled, Route: route, TurnID: turnID})
}

// NoteAskSeen records the ask itself for drivers that do not log it (the
// mock logs asks when injected).
func (m *Manager) NoteAskSeen(deviceID, query string, at time.Time) {
	if _, ok := m.Driver().(selfRecorder); ok {
		return
	}
	m.events.Record(MockEvent{Device: deviceID, Kind: EvAsk, Text: query, TS: at})
}
