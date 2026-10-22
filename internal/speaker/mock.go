package speaker

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// Mock event kinds.
const (
	EvTTS         = "tts"
	EvPlayURL     = "play_url"
	EvPause       = "pause"
	EvPlay        = "play"
	EvStop        = "stop"
	EvVolume      = "volume"
	EvAsk         = "ask"
	EvAskDetected = "ask_detected" // recorded by the hook: latency_ms = detection lag, handled + route
	EvReply       = "reply"
	// Flow events of hook turns (data in MockEvent.Data).
	EvTurnStart   = "turn_start"
	EvLLMStart    = "llm_start"
	EvLLMEnd      = "llm_end"
	EvToolCall    = "tool_call"
	EvToolResult  = "tool_result"
	EvTurnEnd     = "turn_end"
	EvNativeReply = "native_reply" // simulated native reply of an ignored ask (debug device)
)

// ErrInjected is returned by the mock when fault injection fires.
var ErrInjected = errors.New("mock: injected failure")

// Mock is an in-memory driver with two fake devices. It records every action
// in a ring buffer and can simulate latency and failures.
type Mock struct {
	mu      sync.Mutex
	devices []Device
	log     *EventLog
	asks    map[string][]Ask
	lastAsk time.Time
	playing map[string]time.Time // device -> playback end (zero = until paused)
	paused  map[string]bool
	volume  map[string]int
	rng     *rand.Rand

	// Settings (adjustable at any time via Configure).
	latency  time.Duration
	failRate float64
	// CharsPerSec is the simulated TTS speed (0 = instant, status never "playing").
	CharsPerSec float64
	// NativeReply makes InjectAsk start a simulated native answer that the hook
	// has to interrupt.
	NativeReply bool
}

// NewMock creates the mock driver with the single built-in device "debug".
func NewMock() *Mock {
	t := true
	return &Mock{
		devices: []Device{
			{DeviceID: DebugDeviceID, Name: "调试音箱", Hardware: "LX06", Enabled: true, PlayMode: "auto", TTSMode: "auto", Online: &t},
		},
		asks: map[string][]Ask{}, log: NewEventLog(), playing: map[string]time.Time{}, paused: map[string]bool{},
		volume: map[string]int{}, rng: rand.New(rand.NewSource(time.Now().UnixNano())),
		CharsPerSec: 20,
	}
}

// Configure sets latency and failure injection.
func (m *Mock) Configure(latencyMS int, failRate float64) {
	m.mu.Lock()
	m.latency, m.failRate = time.Duration(latencyMS)*time.Millisecond, failRate
	m.mu.Unlock()
}

// Name implements Driver.
func (m *Mock) Name() string { return "mock" }

func (m *Mock) record(ev MockEvent) { m.log.Record(ev) }

// SelfRecords marks that the mock logs to the shared event log itself.
func (m *Mock) SelfRecords() bool { return true }

// UseLog makes the mock record into l (the Manager's shared log).
func (m *Mock) UseLog(l *EventLog) { m.mu.Lock(); m.log = l; m.mu.Unlock() }

// Record appends an event to the log.
func (m *Mock) Record(ev MockEvent) { m.record(ev) }

// Events returns events with Seq > after (oldest first).
func (m *Mock) Events(after int64) []MockEvent {
	m.mu.Lock()
	l := m.log
	m.mu.Unlock()
	return l.Events(after, "", "")
}

// inject applies latency and the failure roll; it returns with m.mu unlocked.
func (m *Mock) inject(ctx context.Context) error {
	m.mu.Lock()
	lat, fail := m.latency, m.failRate
	roll := m.rng.Float64()
	m.mu.Unlock()
	if lat > 0 {
		select {
		case <-time.After(lat):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if fail > 0 && roll < fail {
		return ErrInjected
	}
	return nil
}

// DeviceList implements Driver.
func (m *Mock) DeviceList(ctx context.Context) ([]Device, error) {
	if err := m.inject(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Device(nil), m.devices...), nil
}

// InjectAsk simulates the user talking to the speaker.
func (m *Mock) InjectAsk(deviceID, text string) Ask {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().Truncate(time.Millisecond)
	if !now.After(m.lastAsk) {
		now = m.lastAsk.Add(time.Millisecond)
	}
	m.lastAsk = now
	a := Ask{DeviceID: deviceID, Query: text, Time: now, Native: m.NativeReply}
	m.asks[deviceID] = append(m.asks[deviceID], a)
	m.record(MockEvent{Device: deviceID, Kind: EvAsk, Text: text})
	if m.NativeReply {
		m.playing[deviceID] = time.Time{}
		delete(m.paused, deviceID)
	}
	return a
}

// GetLatestAsk implements Driver.
func (m *Mock) GetLatestAsk(ctx context.Context, d Device, since time.Time) (*Ask, error) {
	if err := m.inject(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.asks[d.DeviceID]
	if len(l) == 0 {
		return nil, nil
	}
	a := l[len(l)-1]
	if !a.Time.After(since) {
		return nil, nil
	}
	return &a, nil
}

// TextToSpeech implements Driver.
func (m *Mock) TextToSpeech(ctx context.Context, d Device, text string) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(MockEvent{Device: d.DeviceID, Kind: EvTTS, Text: text, TurnID: turnFrom(ctx)})
	m.startPlayback(d.DeviceID, len([]rune(text)))
	return nil
}

func (m *Mock) startPlayback(dev string, runes int) {
	delete(m.paused, dev)
	if m.CharsPerSec > 0 {
		m.playing[dev] = time.Now().Add(time.Duration(float64(runes) / m.CharsPerSec * float64(time.Second)))
	} else {
		delete(m.playing, dev)
	}
}

// PlayByURL implements Driver.
func (m *Mock) PlayByURL(ctx context.Context, d Device, url string) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	meta, _ := ctx.Value(playMetaKey{}).(PlayMeta)
	m.record(MockEvent{Device: d.DeviceID, Kind: EvPlayURL, URL: url, Title: meta.Title, Artist: meta.Artist, TurnID: turnFrom(ctx)})
	delete(m.paused, d.DeviceID)
	m.playing[d.DeviceID] = time.Time{}
	return nil
}

// PlayerSetVolume implements Driver.
func (m *Mock) PlayerSetVolume(ctx context.Context, d Device, v int) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volume[d.DeviceID] = v
	m.record(MockEvent{Device: d.DeviceID, Kind: EvVolume, Volume: v, TurnID: turnFrom(ctx)})
	return nil
}

// PlayerPause implements Driver.
func (m *Mock) PlayerPause(ctx context.Context, d Device) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.playing[d.DeviceID]; ok {
		m.paused[d.DeviceID] = true
		delete(m.playing, d.DeviceID)
	}
	m.record(MockEvent{Device: d.DeviceID, Kind: EvPause, TurnID: turnFrom(ctx)})
	return nil
}

// PlayerPlay implements Driver.
func (m *Mock) PlayerPlay(ctx context.Context, d Device) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused[d.DeviceID] {
		delete(m.paused, d.DeviceID)
		m.playing[d.DeviceID] = time.Time{}
	}
	m.record(MockEvent{Device: d.DeviceID, Kind: EvPlay, TurnID: turnFrom(ctx)})
	return nil
}

// PlayerStop implements Driver.
func (m *Mock) PlayerStop(ctx context.Context, d Device) error {
	if err := m.inject(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.playing, d.DeviceID)
	delete(m.paused, d.DeviceID)
	m.record(MockEvent{Device: d.DeviceID, Kind: EvStop, TurnID: turnFrom(ctx)})
	return nil
}

// PlayerGetStatus implements Driver.
func (m *Mock) PlayerGetStatus(ctx context.Context, d Device) (Status, error) {
	if err := m.inject(ctx); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Volume: m.volume[d.DeviceID]}
	if end, ok := m.playing[d.DeviceID]; ok {
		if end.IsZero() || time.Now().Before(end) {
			st.Status = StatusPlaying
		} else {
			delete(m.playing, d.DeviceID)
		}
	}
	if st.Status == StatusIdle && m.paused[d.DeviceID] {
		st.Status = StatusPaused
	}
	return st, nil
}

// StaticDevices returns the fake devices without fault injection.
func (m *Mock) StaticDevices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Device(nil), m.devices...)
}

// SetNativeReply toggles the simulated native answer started by InjectAsk.
func (m *Mock) SetNativeReply(v bool) {
	m.mu.Lock()
	m.NativeReply = v
	m.mu.Unlock()
}

// SetCharsPerSec sets the simulated TTS speed (0 = instant).
func (m *Mock) SetCharsPerSec(v float64) {
	m.mu.Lock()
	m.CharsPerSec = v
	m.mu.Unlock()
}
