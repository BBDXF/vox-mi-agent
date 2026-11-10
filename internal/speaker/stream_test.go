package speaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// streamedMock provides deterministic arrivals and disconnections while using
// the existing simulated player and the real Hook/Manager lifecycle.
type streamedMock struct {
	*Mock
	arrivals  chan streamArrival
	connected chan struct{}
	closed    chan struct{}
}

type streamArrival struct {
	ask Ask
	err error
}

func (s *streamedMock) StreamAsks(ctx context.Context, _ Device, receive func(Ask)) error {
	select {
	case s.connected <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { s.closed <- struct{}{} }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-s.arrivals:
			if ev.err != nil {
				return ev.err
			}
			receive(ev.ask)
		}
	}
}

func awaitStream(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("stream lifecycle did not complete")
	}
}

func TestHookStreamReceivesClockSkewedAsksAndCancelsPreviousTurn(t *testing.T) {
	ag := &fakeAgent{delay: time.Hour}
	r := newRig(t, ag)
	d := &streamedMock{Mock: r.mock, arrivals: make(chan streamArrival, 4), connected: make(chan struct{}, 4), closed: make(chan struct{}, 4)}
	r.mgr.Set(d, nil, 0)
	// A stream must not wait for the cloud polling interval.
	r.hook.Apply(HookSettings{Mode: "all", PollInterval: time.Hour})
	awaitStream(t, d.connected)
	dev := r.mgr.Devices()[0]
	old := time.Now().Add(-24 * time.Hour)
	d.arrivals <- streamArrival{ask: Ask{Query: "第一句", Time: old}}
	waitFor(t, "first live turn", func() bool { return len(ag.Inputs()) == 1 })
	// The same timestamp is valid for two separate delivered utterances.
	d.arrivals <- streamArrival{ask: Ask{Query: "第二句", Time: old}}
	waitFor(t, "second live turn", func() bool { return len(ag.Inputs()) == 2 })
	inputs := ag.Inputs()
	if inputs[0].Text != "第一句" || inputs[1].Text != "第二句" {
		t.Fatalf("arrival order lost: %+v", inputs)
	}
	if last := r.hook.Status().LastAsk; last == nil || last.Device != dev.DeviceID || last.Query != "第二句" {
		t.Fatalf("wrong local device or last ask: %+v", last)
	}
	ag.mu.Lock()
	canceled := ag.canceledN
	ag.mu.Unlock()
	if canceled != 1 {
		t.Fatalf("previous turn was not canceled: %d", canceled)
	}
	r.hook.Stop()
	awaitStream(t, d.closed)
}

func TestHookStreamReconnectAndDisable(t *testing.T) {
	ag := &fakeAgent{}
	r := newRig(t, ag)
	d := &streamedMock{Mock: r.mock, arrivals: make(chan streamArrival, 4), connected: make(chan struct{}, 4), closed: make(chan struct{}, 4)}
	r.mgr.Set(d, nil, 0)
	r.hook.Apply(HookSettings{Mode: "all", PollInterval: time.Hour})
	awaitStream(t, d.connected)
	d.arrivals <- streamArrival{err: errors.New("connection lost")}
	awaitStream(t, d.closed)
	awaitStream(t, d.connected)
	d.arrivals <- streamArrival{ask: Ask{Query: "连接恢复后的问题"}}
	waitFor(t, "turn after reconnect", func() bool { return len(ag.Inputs()) == 1 })
	if got := ag.Inputs()[0].Text; got != "连接恢复后的问题" {
		t.Fatalf("unexpected replay or lost input: %q", got)
	}
	r.hook.Apply(HookSettings{Mode: "off"})
	awaitStream(t, d.closed)
	if r.hook.Status().Running {
		t.Fatal("disabled hook kept listening")
	}
}
