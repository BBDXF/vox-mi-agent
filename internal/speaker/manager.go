package speaker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/tools/audio"
)

// DebugDeviceID is the always-available mock device (调试音箱).
const DebugDeviceID = "debug"

// Manager routes speaker operations by device id: the debug device goes to
// the debug (mock) driver, every other device to the configured driver. It
// implements audio.Player (PlayByURL / volume on the default device).
type Manager struct {
	mu      sync.RWMutex
	drv     Driver // configured driver as callers use it (wrapped to log its actions)
	inner   Driver
	debug   Driver // debug device driver (nil = the configured driver handles it)
	debugOn bool
	events  *EventLog
	devices []Device // configured devices
	volume  int
	ver     int
	log     *slog.Logger
	rt      *router
}

var _ audio.Player = (*Manager)(nil)

// NewManager creates a Manager with a mock driver until Set is called.
func NewManager(log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{log: log, events: NewEventLog(), debugOn: true}
	m.rt = &router{m: m}
	m.setDriverLocked(NewMock())
	return m
}

func (m *Manager) setDriverLocked(d Driver) {
	if d == m.inner && m.drv != nil {
		return
	}
	m.ver++
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

// SetDebug installs the debug device driver (a self-recording mock) and
// whether the device is enabled (speaker.debug_device).
func (m *Manager) SetDebug(d Driver, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.debug != d || m.debugOn != enabled {
		m.ver++
	}
	m.debug, m.debugOn = d, enabled
	if mk, ok := d.(interface{ UseLog(*EventLog) }); ok {
		mk.UseLog(m.events)
	}
}

// Version changes whenever the driver set or the debug setting changes.
func (m *Manager) Version() int { m.mu.RLock(); defer m.mu.RUnlock(); return m.ver }

// Log returns the shared event log (all drivers).
func (m *Manager) Log() *EventLog { return m.events }

// Set swaps the configured driver and device list (hot reload).
func (m *Manager) Set(drv Driver, devices []Device, volumeDefault int) {
	m.mu.Lock()
	m.setDriverLocked(drv)
	m.volume = volumeDefault
	m.devices = append([]Device(nil), devices...)
	m.mu.Unlock()
}

// Driver returns the routing driver (by device id).
func (m *Manager) Driver() Driver { return m.rt }

// target is the driver that handles deviceID.
func (m *Manager) target(deviceID string) Driver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if deviceID == DebugDeviceID && m.debug != nil {
		return m.debug
	}
	return m.drv
}

// router implements Driver by delegating per device id.
type router struct{ m *Manager }

func (r *router) Name() string {
	r.m.mu.RLock()
	defer r.m.mu.RUnlock()
	return r.m.inner.Name()
}
func (r *router) DeviceList(ctx context.Context) ([]Device, error) {
	r.m.mu.RLock()
	d := r.m.drv
	r.m.mu.RUnlock()
	return d.DeviceList(ctx)
}
func (r *router) GetLatestAsk(ctx context.Context, d Device, since time.Time) (*Ask, error) {
	return r.m.target(d.DeviceID).GetLatestAsk(ctx, d, since)
}
func (r *router) TextToSpeech(ctx context.Context, d Device, t string) error {
	return r.m.target(d.DeviceID).TextToSpeech(ctx, d, t)
}
func (r *router) PlayByURL(ctx context.Context, d Device, u string) error {
	return r.m.target(d.DeviceID).PlayByURL(ctx, d, u)
}
func (r *router) PlayerSetVolume(ctx context.Context, d Device, v int) error {
	return r.m.target(d.DeviceID).PlayerSetVolume(ctx, d, v)
}
func (r *router) PlayerPause(ctx context.Context, d Device) error {
	return r.m.target(d.DeviceID).PlayerPause(ctx, d)
}
func (r *router) PlayerPlay(ctx context.Context, d Device) error {
	return r.m.target(d.DeviceID).PlayerPlay(ctx, d)
}
func (r *router) PlayerStop(ctx context.Context, d Device) error {
	return r.m.target(d.DeviceID).PlayerStop(ctx, d)
}
func (r *router) PlayerGetStatus(ctx context.Context, d Device) (Status, error) {
	return r.m.target(d.DeviceID).PlayerGetStatus(ctx, d)
}

// VolumeDefault returns speaker.volume_default.
func (m *Manager) VolumeDefault() int { m.mu.RLock(); defer m.mu.RUnlock(); return m.volume }

// Configured returns the configured devices (enabled or not).
func (m *Manager) Configured() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Device(nil), m.devices...)
}

func (m *Manager) debugDeviceLocked() (Device, bool) {
	if m.debug == nil || !m.debugOn {
		return Device{}, false
	}
	if s, ok := m.debug.(interface{ StaticDevices() []Device }); ok {
		for _, d := range s.StaticDevices() {
			if d.DeviceID == DebugDeviceID {
				return d, true
			}
		}
	}
	return Device{DeviceID: DebugDeviceID, Name: "调试音箱", Hardware: "LX06", Enabled: true, PlayMode: "auto", TTSMode: "auto"}, true
}

func (m *Manager) listLocked(onlyEnabled bool) []Device {
	var out []Device
	seen := map[string]bool{}
	add := func(d Device) {
		if !seen[d.DeviceID] {
			seen[d.DeviceID] = true
			out = append(out, d)
		}
	}
	for _, d := range m.devices {
		if d.DeviceID != DebugDeviceID && (d.Enabled || !onlyEnabled) {
			add(d)
		}
	}
	if len(m.devices) == 0 {
		if s, ok := m.inner.(interface{ StaticDevices() []Device }); ok {
			for _, d := range s.StaticDevices() {
				if d.DeviceID != DebugDeviceID || m.debug == nil {
					add(d)
				}
			}
		}
	}
	if d, ok := m.debugDeviceLocked(); ok {
		add(d)
	}
	return out
}

// Devices returns the effective enabled devices: configured real devices
// first (or the log driver's pseudo device), then the debug device.
func (m *Manager) Devices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listLocked(true)
}

// AllDevices is Devices including configured-but-disabled ones.
func (m *Manager) AllDevices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listLocked(false)
}

// Resolve finds a device by id ("" = the first enabled device: a real one if
// any, else the debug device).
func (m *Manager) Resolve(id string) (Device, error) {
	if id == "" {
		l := m.Devices()
		if len(l) == 0 {
			return Device{}, ErrNoDevice
		}
		return l[0], nil
	}
	for _, d := range m.AllDevices() {
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
func (m *Manager) NoteAsk(deviceID, query string, lag time.Duration, handled bool, route, turnID string, data map[string]any) {
	ms := max(lag.Milliseconds(), 0)
	m.events.Record(MockEvent{Device: deviceID, Kind: EvAskDetected, Text: query, LatencyMS: &ms, Handled: &handled, Route: route, TurnID: turnID, Data: data})
}

// NoteAskSeen records the ask itself for drivers that do not log it (the
// mock logs asks when injected).
func (m *Manager) NoteAskSeen(deviceID, query string, at time.Time) {
	if _, ok := m.target(deviceID).(selfRecorder); ok {
		return
	}
	m.events.Record(MockEvent{Device: deviceID, Kind: EvAsk, Text: query, TS: at})
}
