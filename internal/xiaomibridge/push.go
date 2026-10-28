package xiaomibridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Pusher POSTs events to the configured URL from a single goroutine, in order.
// Delivery is best-effort: no retries (a late ask is worse than a lost one)
// and events are dropped when the queue is full. URL, token and the partial
// flag are read from the settings at the time of each event.
type Pusher struct {
	settings func() PushSettings
	client   *http.Client
	timeout  time.Duration
	log      *slog.Logger
	q        chan Event

	mu    sync.Mutex
	stats PushStats
}

// PushStats are reported by /v1/status.
type PushStats struct {
	Sent      int64  `json:"sent"`
	Failed    int64  `json:"failed"`
	Dropped   int64  `json:"dropped"`
	LastError string `json:"last_error,omitempty"`
	LastOKAt  int64  `json:"last_ok_at,omitempty"` // unix ms
}

const pushQueue = 64

// NewPusher creates a pusher. A nil client means http.DefaultClient.
func NewPusher(settings func() PushSettings, client *http.Client, timeout time.Duration, log *slog.Logger) *Pusher {
	if client == nil {
		client = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Pusher{settings: settings, client: client, timeout: timeout, log: log, q: make(chan Event, pushQueue)}
}

// Run delivers queued events until ctx is done.
func (p *Pusher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-p.q:
			s := p.settings()
			if s.URL == "" { // disabled while queued
				continue
			}
			err := p.post(ctx, s, ev)
			if ctx.Err() != nil {
				return
			}
			p.mu.Lock()
			if err != nil {
				p.stats.Failed++
				p.stats.LastError = err.Error()
			} else {
				p.stats.Sent++
				p.stats.LastOKAt = time.Now().UnixMilli()
			}
			p.mu.Unlock()
			if err != nil {
				p.log.Warn("push failed", "seq", ev.Seq, "err", err)
			}
		}
	}
}

// Offer queues ev if push is enabled and ev qualifies; it never blocks.
func (p *Pusher) Offer(ev Event) {
	s := p.settings()
	if s.URL == "" || (!ev.Final && !s.Partial) {
		return
	}
	select {
	case p.q <- ev:
	default:
		p.mu.Lock()
		p.stats.Dropped++
		p.mu.Unlock()
		p.log.Warn("push queue full, event dropped", "seq", ev.Seq)
	}
}

// Stats returns a snapshot of the delivery counters.
func (p *Pusher) Stats() PushStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

func (p *Pusher) post(ctx context.Context, s PushSettings, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // keep the connection reusable
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
