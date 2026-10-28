package xiaomibridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Event is the JSON body POSTed to the push URL.
type Event struct {
	Type     string `json:"type"` // always "asr"
	Device   string `json:"device"`
	DialogID string `json:"dialog_id"`
	Text     string `json:"text"`
	Final    bool   `json:"final"`
	TS       int64  `json:"ts"` // unix ms when the bridge read the line
}

// Pusher delivers events in order from a single goroutine. Delivery is
// best-effort: no retries (a late ask is worse than a lost one) and events are
// dropped when the queue is full.
type Pusher struct {
	url, token string
	client     *http.Client
	timeout    time.Duration
	log        *slog.Logger
	q          chan Event
}

const pushQueue = 64

// NewPusher creates a pusher; token is sent as "Authorization: Bearer <token>"
// when non-empty. A nil client means http.DefaultClient.
func NewPusher(url, token string, client *http.Client, timeout time.Duration, log *slog.Logger) *Pusher {
	if client == nil {
		client = http.DefaultClient
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Pusher{url: url, token: token, client: client, timeout: timeout, log: log, q: make(chan Event, pushQueue)}
}

// Run delivers queued events until ctx is done.
func (p *Pusher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-p.q:
			if err := p.post(ctx, ev); err != nil && ctx.Err() == nil {
				p.log.Warn("push failed", "dialog_id", ev.DialogID, "err", err)
			}
		}
	}
}

// Enqueue queues ev without blocking; it reports false when ev was dropped.
func (p *Pusher) Enqueue(ev Event) bool {
	select {
	case p.q <- ev:
		return true
	default:
		p.log.Warn("push queue full, event dropped", "dialog_id", ev.DialogID)
		return false
	}
}

func (p *Pusher) post(ctx context.Context, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
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
