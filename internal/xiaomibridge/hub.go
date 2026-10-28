package xiaomibridge

import (
	"context"
	"sync"
)

// Event is one recognized user utterance, as pushed, streamed and returned.
type Event struct {
	Seq      int64  `json:"seq"`  // increasing per bridge run (restarts reset it)
	Type     string `json:"type"` // always "asr"
	Device   string `json:"device"`
	DialogID string `json:"dialog_id"`
	Text     string `json:"text"`
	Final    bool   `json:"final"`
	TS       int64  `json:"ts"` // unix ms when the bridge read the line
}

// Hub fans recognition events out to stream subscribers and keeps the latest
// final one.
type Hub struct {
	mu     sync.Mutex
	seq    int64
	finals int64
	latest *Event
	subs   map[*sub]struct{}
}

type sub struct {
	ch      chan Event
	partial bool
}

const subBuffer = 16

func NewHub() *Hub { return &Hub{subs: map[*sub]struct{}{}} }

// Publish assigns ev its Seq and delivers it. Slow subscribers miss events
// rather than block the log reader.
func (h *Hub) Publish(ev Event) Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ev.Seq = h.seq
	if ev.Final {
		h.finals++
		e := ev
		h.latest = &e
	}
	for s := range h.subs {
		if !ev.Final && !s.partial {
			continue
		}
		select {
		case s.ch <- ev:
		default:
		}
	}
	return ev
}

// Subscribe returns a channel of future events, the latest final event at the
// moment of subscribing (nil if none) and a cancel func.
func (h *Hub) Subscribe(partial bool) (<-chan Event, *Event, func()) {
	s := &sub{ch: make(chan Event, subBuffer), partial: partial}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	latest := h.latest
	h.mu.Unlock()
	return s.ch, latest, func() {
		h.mu.Lock()
		delete(h.subs, s)
		h.mu.Unlock()
	}
}

// Latest returns the latest final event (nil if none) and the number of final
// events so far.
func (h *Hub) Latest() (*Event, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest, h.finals
}

// Subscribers returns the number of open subscriptions.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// WaitFinal returns the first final event with Seq > afterSeq, waiting until
// one arrives or ctx is done (ok=false).
func (h *Hub) WaitFinal(ctx context.Context, afterSeq int64) (Event, bool) {
	ch, latest, cancel := h.Subscribe(false)
	defer cancel()
	if latest != nil && latest.Seq > afterSeq {
		return *latest, true
	}
	for {
		select {
		case <-ctx.Done():
			return Event{}, false
		case ev := <-ch:
			if ev.Seq > afterSeq {
				return ev, true
			}
		}
	}
}
