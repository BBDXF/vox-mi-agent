package speaker

import (
	"context"
	"sync"
	"time"
)

const eventRing = 2000

// MockEvent is one entry of the speaker event log (all drivers).
// (Name kept for history; see SpeakerEvent.)
type MockEvent struct {
	Seq    int64     `json:"seq"`
	TS     time.Time `json:"ts"`
	Device string    `json:"device_id"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text,omitempty"`
	URL    string    `json:"url,omitempty"`
	Volume int       `json:"volume,omitempty"`
	// LatencyMS is the time since the latest ask on this device (set on every
	// event except "ask", within 60s of that ask).
	LatencyMS *int64 `json:"latency_ms,omitempty"`
	// play_url: track metadata (when known).
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	// ask_detected: the hook's decision.
	Handled *bool  `json:"handled,omitempty"`
	Route   string `json:"route,omitempty"` // direct|artist_random|agent|ignored
	// ConversationID is the device's speaker conversation, when known.
	ConversationID string `json:"conversation_id,omitempty"`
	// TurnID is the hook turn the event belongs to, when known.
	TurnID string `json:"turn_id,omitempty"`
}

// SpeakerEvent is the neutral name of MockEvent.
type SpeakerEvent = MockEvent

type turnKey struct{}

// WithTurnID attaches the hook turn id to ctx; drivers' events carry it.
func WithTurnID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnKey{}, id)
}

func turnFrom(ctx context.Context) string { s, _ := ctx.Value(turnKey{}).(string); return s }

// EventLog is the shared ring buffer of speaker events.
type EventLog struct {
	mu      sync.Mutex
	events  []MockEvent
	seq     int64
	askAt   map[string]time.Time
	convFor func(device string) string
}

// NewEventLog creates an empty log.
func NewEventLog() *EventLog { return &EventLog{askAt: map[string]time.Time{}} }

// SetConvLookup sets how events find their device's conversation id.
func (l *EventLog) SetConvLookup(f func(device string) string) {
	l.mu.Lock()
	l.convFor = f
	l.mu.Unlock()
}

// Record appends ev (Seq, TS, latency and conversation are filled in).
func (l *EventLog) Record(ev MockEvent) {
	l.mu.Lock()
	f := l.convFor
	l.mu.Unlock()
	if ev.ConversationID == "" && f != nil && ev.Device != "" {
		ev.ConversationID = f(ev.Device)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	ev.Seq = l.seq
	if ev.TS.IsZero() {
		ev.TS = time.Now()
	}
	if ev.Kind == EvAsk {
		l.askAt[ev.Device] = ev.TS
	} else if t, ok := l.askAt[ev.Device]; ok && ev.LatencyMS == nil {
		if d := ev.TS.Sub(t).Milliseconds(); d >= 0 && d <= 60000 {
			ev.LatencyMS = &d
		}
	}
	l.events = append(l.events, ev)
	if len(l.events) > eventRing {
		l.events = append([]MockEvent(nil), l.events[len(l.events)-eventRing:]...)
	}
}

// Events returns events with Seq > after, oldest first, optionally filtered
// by device and/or conversation ("" = any).
func (l *EventLog) Events(after int64, device, conversation string) []MockEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []MockEvent
	for _, e := range l.events {
		if e.Seq > after && (device == "" || e.Device == device) && (conversation == "" || e.ConversationID == conversation) {
			out = append(out, e)
		}
	}
	return out
}

// LastSeq is the newest sequence number.
func (l *EventLog) LastSeq() int64 { l.mu.Lock(); defer l.mu.Unlock(); return l.seq }

// recordingDriver logs the actions of a driver that does not log itself.
type recordingDriver struct {
	Driver
	log *EventLog
}

func (r *recordingDriver) rec(ctx context.Context, ev MockEvent) {
	ev.TurnID = turnFrom(ctx)
	r.log.Record(ev)
}

func (r *recordingDriver) TextToSpeech(ctx context.Context, d Device, text string) error {
	err := r.Driver.TextToSpeech(ctx, d, text)
	if err == nil {
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvTTS, Text: text})
	}
	return err
}

func (r *recordingDriver) PlayByURL(ctx context.Context, d Device, url string) error {
	err := r.Driver.PlayByURL(ctx, d, url)
	if err == nil {
		meta, _ := ctx.Value(playMetaKey{}).(PlayMeta)
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvPlayURL, URL: url, Title: meta.Title, Artist: meta.Artist})
	}
	return err
}

func (r *recordingDriver) PlayerSetVolume(ctx context.Context, d Device, v int) error {
	err := r.Driver.PlayerSetVolume(ctx, d, v)
	if err == nil {
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvVolume, Volume: v})
	}
	return err
}

func (r *recordingDriver) PlayerPause(ctx context.Context, d Device) error {
	err := r.Driver.PlayerPause(ctx, d)
	if err == nil {
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvPause})
	}
	return err
}

func (r *recordingDriver) PlayerPlay(ctx context.Context, d Device) error {
	err := r.Driver.PlayerPlay(ctx, d)
	if err == nil {
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvPlay})
	}
	return err
}

func (r *recordingDriver) PlayerStop(ctx context.Context, d Device) error {
	err := r.Driver.PlayerStop(ctx, d)
	if err == nil {
		r.rec(ctx, MockEvent{Device: d.DeviceID, Kind: EvStop})
	}
	return err
}

// selfRecorder is implemented by drivers that log to the shared EventLog
// themselves (the mock).
type selfRecorder interface{ SelfRecords() bool }
