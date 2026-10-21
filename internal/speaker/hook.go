package speaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/store"
)

// TurnRunner is the part of *agent.Runtime the hook needs.
type TurnRunner interface {
	Run(ctx context.Context, in agent.Input, emit func(agent.Event)) error
	CancelConversation(id string) bool
}

// HookSettings mirror speaker.hook.
type HookSettings struct {
	Mode            string // keyword|all|off
	Keywords        []string
	PollInterval    time.Duration
	InterruptWindow time.Duration
}

// LastAsk is the most recent ask seen by the hook.
type LastAsk struct {
	Device  string    `json:"device"`
	Query   string    `json:"query"`
	Time    time.Time `json:"time"`
	Handled bool      `json:"handled"`
}

// HookStatus is served by /api/speaker/status.
type HookStatus struct {
	Running bool     `json:"running"`
	LastAsk *LastAsk `json:"last_ask,omitempty"`
}

// HookDeps are the hook's collaborators.
type HookDeps struct {
	Mgr    *Manager
	Agent  TurnRunner
	Store  *store.Store // conversation + settings persistence
	Tracer agent.Tracer // optional
	Log    *slog.Logger
}

// Hook polls the enabled devices for user asks and answers them with the agent.
type Hook struct {
	d HookDeps

	// Timing knobs (tests shorten them).
	RecheckEvery time.Duration // interrupt recheck period (300ms)
	StatusPoll   time.Duration // playback status poll (300ms)
	StartGrace   time.Duration // wait for "playing" to show up (1.5s)
	CharsPerSec  float64       // fallback speech speed (4.5)

	mu      sync.Mutex
	cur     HookSettings
	curDrv  Driver
	curDevs []Device
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	turns   map[string]*turn
	convs   map[string]string
	last    *LastAsk
	running bool
}

type turn struct {
	cancel context.CancelFunc
	done   chan struct{}
	conv   string
}

// NewHook creates a stopped hook.
func NewHook(d HookDeps) *Hook {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Hook{d: d, RecheckEvery: 300 * time.Millisecond, StatusPoll: 300 * time.Millisecond,
		StartGrace: 1500 * time.Millisecond, CharsPerSec: 4.5, turns: map[string]*turn{}, convs: map[string]string{}}
}

// Status returns the hook state.
func (h *Hook) Status() HookStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := HookStatus{Running: h.running}
	if h.last != nil {
		l := *h.last
		st.LastAsk = &l
	}
	return st
}

// Apply (re)starts the pollers for the manager's current driver and enabled
// devices. It is a no-op when nothing changed. Mode "off" stops everything.
func (h *Hook) Apply(s HookSettings) {
	drv, devs := h.d.Mgr.Driver(), h.d.Mgr.Devices()
	h.mu.Lock()
	if h.running == (s.Mode != "off") && reflect.DeepEqual(s, h.cur) && drv == h.curDrv && reflect.DeepEqual(devs, h.curDevs) {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Stop()
	h.mu.Lock()
	h.cur, h.curDrv, h.curDevs = s, drv, devs
	if s.Mode == "off" || s.Mode == "" || len(devs) == 0 || drv == nil || drv.Name() == "log" {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.running = cancel, true
	since := time.Now().Truncate(time.Millisecond).Add(-time.Millisecond) // ask times have ms precision
	for _, d := range devs {
		h.wg.Add(1)
		go h.poll(ctx, drv, d, s, since)
	}
	h.mu.Unlock()
}

// Stop stops all pollers and running turns and waits for them.
func (h *Hook) Stop() {
	h.mu.Lock()
	cancel := h.cancel
	h.cancel, h.running = nil, false
	turns := make([]*turn, 0, len(h.turns))
	for _, t := range h.turns {
		turns = append(turns, t)
	}
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, t := range turns {
		t.cancel()
		<-t.done
	}
	h.wg.Wait()
}

func (h *Hook) poll(ctx context.Context, drv Driver, dev Device, s HookSettings, since time.Time) {
	defer h.wg.Done()
	errs := 0
	for {
		wait := s.PollInterval
		if errs > 0 {
			wait *= time.Duration(min(errs, 10))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		ask, err := drv.GetLatestAsk(ctx, dev, since)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errs++
			if errs == 1 || errs%30 == 0 {
				h.d.Log.Warn("speaker hook: poll failed", "device", dev.DeviceID, "err", err)
			}
			continue
		}
		errs = 0
		if ask == nil || ask.Query == "" || !ask.Time.After(since) {
			continue
		}
		since = ask.Time
		h.handle(ctx, drv, dev, s, *ask)
	}
}

// stripKeyword returns the query without its wake prefix.
func stripKeyword(q string, kws []string) (string, bool) {
	q = strings.TrimSpace(q)
	k := append([]string(nil), kws...)
	sort.Slice(k, func(i, j int) bool { return len(k[i]) > len(k[j]) })
	for _, kw := range k {
		kw = strings.TrimSpace(kw)
		if kw != "" && strings.HasPrefix(q, kw) {
			rest := strings.TrimLeft(strings.TrimPrefix(q, kw), " \t,，。.、:：!！?？")
			return rest, rest != ""
		}
	}
	return "", false
}

func (h *Hook) handle(ctx context.Context, drv Driver, dev Device, s HookSettings, ask Ask) {
	t0 := time.Now()
	// A new ask always ends the running turn.
	h.cancelTurn(dev.DeviceID)
	text, ok := ask.Query, true
	if s.Mode == "keyword" {
		text, ok = stripKeyword(ask.Query, s.Keywords)
	}
	h.mu.Lock()
	h.last = &LastAsk{Device: dev.DeviceID, Query: ask.Query, Time: ask.Time, Handled: ok}
	h.mu.Unlock()
	h.d.Log.Info("speaker hook: ask", "device", dev.DeviceID, "query", ask.Query, "handled", ok)
	if !ok {
		return
	}
	conv, err := h.conversation(ctx, dev)
	if err != nil {
		h.d.Log.Error("speaker hook: conversation", "device", dev.DeviceID, "err", err)
		return
	}
	tctx, cancel := context.WithCancel(ctx)
	t := &turn{cancel: cancel, done: make(chan struct{}), conv: conv}
	h.mu.Lock()
	h.turns[dev.DeviceID] = t
	h.mu.Unlock()
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer close(t.done)
		defer cancel()
		defer func() {
			h.mu.Lock()
			if h.turns[dev.DeviceID] == t {
				delete(h.turns, dev.DeviceID)
			}
			h.mu.Unlock()
		}()
		h.runTurn(tctx, drv, dev, s, conv, text, ask, t0)
	}()
}

func (h *Hook) cancelTurn(devID string) {
	h.mu.Lock()
	t := h.turns[devID]
	h.mu.Unlock()
	if t == nil {
		return
	}
	t.cancel()
	h.d.Agent.CancelConversation(t.conv)
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
	}
}

func (h *Hook) conversation(ctx context.Context, dev Device) (string, error) {
	h.mu.Lock()
	id := h.convs[dev.DeviceID]
	h.mu.Unlock()
	if id != "" {
		return id, nil
	}
	if h.d.Store == nil {
		return "", errors.New("no store")
	}
	key := "speaker.conversation." + dev.DeviceID
	if v, err := h.d.Store.GetSetting(ctx, key); err == nil && v != "" {
		if _, err := h.d.Store.GetConversation(ctx, v); err == nil {
			id = v
		}
	}
	if id == "" {
		c, err := h.d.Store.CreateConversation(ctx, firstNonEmpty(dev.Name, dev.DeviceID), store.ChannelSpeaker)
		if err != nil {
			return "", err
		}
		id = c.ID
		if err := h.d.Store.SetSetting(ctx, key, id); err != nil {
			h.d.Log.Warn("speaker hook: persist conversation id", "err", err)
		}
	}
	h.mu.Lock()
	h.convs[dev.DeviceID] = id
	h.mu.Unlock()
	return id, nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// spans records the latency trace of one turn.
type spans struct {
	h      *Hook
	id     string
	conv   string
	query  string
	t0     time.Time
	mu     sync.Mutex
	marks  map[string]int64
	nextID int
}

func (sp *spans) mark(name string, since time.Time, status string) {
	sp.markMS(name, max(time.Since(since).Milliseconds(), 0), status)
}

func (sp *spans) markMS(name string, ms int64, status string) {
	sp.mu.Lock()
	if _, dup := sp.marks[name]; dup {
		sp.mu.Unlock()
		return
	}
	sp.marks[name] = ms
	sp.nextID++
	step := sp.nextID
	sp.mu.Unlock()
	if tr := sp.h.d.Tracer; tr != nil {
		tr.Trace(store.TraceRow{TS: time.Now(), ConversationID: sp.conv, TurnID: sp.id, Step: step,
			Kind: store.TraceTool, Name: "speaker." + name, Status: status, DurationMS: ms,
			Args: store.TruncateText(sp.query, 200), Channel: store.ChannelSpeaker})
	}
}

func (h *Hook) runTurn(ctx context.Context, drv Driver, dev Device, s HookSettings, conv, text string, ask Ask, t0 time.Time) {
	var rb [4]byte
	_, _ = rand.Read(rb[:])
	sp := &spans{h: h, id: "spk-" + hex.EncodeToString(rb[:]), conv: conv, query: text, t0: t0, marks: map[string]int64{}}
	// Detection lag since the device recorded the ask (clock skew clamped).
	sp.markMS("ask_detected", max(0, min(time.Since(ask.Time).Milliseconds(), 60000)), store.TraceOK)

	firstSentence := make(chan struct{})
	var firstOnce sync.Once
	intDone := make(chan struct{})
	go func() {
		defer close(intDone)
		h.interrupt(ctx, drv, dev, s.InterruptWindow, firstSentence)
		sp.mark("interrupt", t0, store.TraceOK)
	}()

	pq := &PlayQueue{}
	sent := make(chan string, 64)
	go func() {
		defer close(sent)
		err := h.d.Agent.Run(WithPlayQueue(ctx, pq), agent.Input{ConversationID: conv, Channel: store.ChannelSpeaker, Text: text}, func(ev agent.Event) {
			switch ev.Type {
			case agent.EventToken:
				sp.mark("llm_first_token", t0, store.TraceOK)
			case agent.EventSentence:
				if sd, ok := ev.Data.(agent.SentenceData); ok && strings.TrimSpace(sd.Text) != "" {
					firstOnce.Do(func() { close(firstSentence) })
					select {
					case sent <- sd.Text:
					case <-ctx.Done():
					}
				}
			case agent.EventError:
				if ed, ok := ev.Data.(agent.ErrorData); ok {
					h.d.Log.Warn("speaker hook: agent error", "device", dev.DeviceID, "err", ed.Message)
				}
			}
		})
		if err != nil && ctx.Err() == nil {
			h.d.Log.Warn("speaker hook: agent run failed", "device", dev.DeviceID, "err", err)
		}
		firstOnce.Do(func() { close(firstSentence) })
	}()

	var reply strings.Builder
	var prev string
	for sentence := range sent {
		select {
		case <-intDone:
		case <-ctx.Done():
			return
		}
		if prev != "" {
			if !h.waitFinish(ctx, drv, dev, prev) {
				return
			}
		}
		if err := drv.TextToSpeech(ctx, dev, sentence); err != nil {
			if ctx.Err() != nil {
				return
			}
			h.d.Log.Warn("speaker hook: tts failed", "device", dev.DeviceID, "err", err)
			continue
		}
		sp.mark("first_tts", t0, store.TraceOK)
		reply.WriteString(sentence)
		prev = sentence
	}
	if reply.Len() > 0 {
		h.d.Mgr.NoteReply(dev.DeviceID, reply.String())
	}
	// A queued play_audio starts only after the last sentence has been spoken.
	if req := pq.Take(); req != nil && ctx.Err() == nil {
		select {
		case <-intDone:
		case <-ctx.Done():
			return
		}
		if prev == "" || h.waitFinish(ctx, drv, dev, prev) {
			if err := drv.PlayByURL(ctx, dev, req.URL); err != nil && ctx.Err() == nil {
				h.d.Log.Warn("speaker hook: queued play failed", "device", dev.DeviceID, "err", err)
			}
		}
	}
	sp.mu.Lock()
	m := make(map[string]int64, len(sp.marks))
	for k, v := range sp.marks {
		m[k] = v
	}
	sp.mu.Unlock()
	h.d.Log.Info("speaker turn", "device", dev.DeviceID, "turn_id", sp.id, "ask_lag_ms", m["ask_detected"],
		"interrupt_ms", m["interrupt"], "llm_first_token_ms", m["llm_first_token"], "first_tts_ms", m["first_tts"],
		"canceled", ctx.Err() != nil)
}

// interrupt pauses the speaker's native reply and re-checks within window
// until the first agent sentence is ready.
func (h *Hook) interrupt(ctx context.Context, drv Driver, dev Device, window time.Duration, stop <-chan struct{}) {
	if err := drv.PlayerPause(ctx, dev); err != nil && ctx.Err() == nil {
		h.d.Log.Warn("speaker hook: pause failed", "device", dev.DeviceID, "err", err)
	}
	if window <= 0 {
		return
	}
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	tick := time.NewTicker(h.RecheckEvery)
	defer tick.Stop()
	rechecks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-deadline.C:
			return
		case <-tick.C:
			st, err := drv.PlayerGetStatus(ctx, dev)
			if err != nil || !st.Playing() {
				continue
			}
			rechecks++
			if rechecks == 1 {
				_ = drv.PlayerPause(ctx, dev)
			} else {
				_ = drv.PlayerStop(ctx, dev)
			}
		}
	}
}

// waitFinish blocks until the sentence has been spoken (status polling, with
// a characters-per-second estimate as fallback). False = ctx canceled.
func (h *Hook) waitFinish(ctx context.Context, drv Driver, dev Device, text string) bool {
	est := time.Duration(float64(len([]rune(text))) / h.CharsPerSec * float64(time.Second))
	start := time.Now()
	limit := est*2 + 3*time.Second
	sleep := func(d time.Duration) bool {
		if d <= 0 {
			return ctx.Err() == nil
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	seen := false
	for {
		el := time.Since(start)
		if el >= limit {
			return ctx.Err() == nil
		}
		st, err := drv.PlayerGetStatus(ctx, dev)
		switch {
		case err != nil:
			return sleep(est - el)
		case st.Playing():
			seen = true
		case seen:
			return ctx.Err() == nil
		case el >= h.StartGrace:
			return sleep(est - el) // never saw it playing: trust the estimate
		}
		if !sleep(h.StatusPoll) {
			return false
		}
	}
}
