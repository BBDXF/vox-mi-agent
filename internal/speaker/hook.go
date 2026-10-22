package speaker

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/media"
	"github.com/andy/vox-mi-agent/internal/store"
)

// TurnRunner is the part of *agent.Runtime the hook needs.
type TurnRunner interface {
	Run(ctx context.Context, in agent.Input, emit func(agent.Event)) error
	CancelConversation(id string) bool
}

// HookSettings mirror speaker.hook.
type HookSettings struct {
	Mode            string // music|keyword|all|off
	Keywords        []string
	WakeWords       []string
	MusicTriggers   []string
	MusicExclude    []string
	DirectPlayScore int // search score (1-100) needed to skip the LLM
	PollInterval    time.Duration
	InterruptWindow time.Duration
}

// LastAsk is the most recent ask seen by the hook.
type LastAsk struct {
	Device  string    `json:"device"`
	Query   string    `json:"query"`
	Time    time.Time `json:"time"`
	Handled bool      `json:"handled"`
	Route   string    `json:"route"` // direct|artist_random|agent|ignored
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
	Music  *MusicDeps // optional: enables the LLM-free music routes
}

// MusicDeps give the hook access to the media library (direct play).
type MusicDeps struct {
	Search   func(ctx context.Context, query string, topN, minScore int) ([]media.Hit, bool, error)
	Tracks   func() []media.Track
	Lookup   func(id string) (media.Track, bool)
	URLFor   func(media.Track) (string, error)
	MinScore func() int // live search.min_score
}

// Hook polls the enabled devices for user asks and answers them with the agent.
type Hook struct {
	d HookDeps

	// Timing knobs (tests shorten them).
	RecheckEvery time.Duration   // interrupt recheck period (300ms)
	StatusPoll   time.Duration   // playback status poll (300ms)
	StartGrace   time.Duration   // wait for "playing" to show up (1.5s)
	CharsPerSec  float64         // fallback speech speed (4.5)
	Rand         func(n int) int // random pick (default math/rand)

	mu      sync.Mutex
	cur     HookSettings
	curVer  int
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
	h := &Hook{d: d, RecheckEvery: 300 * time.Millisecond, StatusPoll: 300 * time.Millisecond,
		StartGrace: 1500 * time.Millisecond, CharsPerSec: 4.5, turns: map[string]*turn{}, convs: map[string]string{}}
	if d.Mgr != nil {
		d.Mgr.Log().SetConvLookup(h.LookupConversation)
	}
	return h
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
	drv, devs, ver := h.d.Mgr.Driver(), h.d.Mgr.Devices(), h.d.Mgr.Version()
	h.mu.Lock()
	if h.running == (s.Mode != "off") && reflect.DeepEqual(s, h.cur) && ver == h.curVer && reflect.DeepEqual(devs, h.curDevs) {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Stop()
	h.mu.Lock()
	h.cur, h.curVer, h.curDevs = s, ver, devs
	if s.Mode == "off" || s.Mode == "" || len(devs) == 0 {
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

// decision is what the hook does with an ask.
type decision struct {
	route string
	text  string // agent route: the user text
	// direct / artist_random:
	track    media.Track
	reply    string
	searchMS int64
	toolArgs string
	toolSum  string
	toolName string         // synthetic search tool name (search_audio)
	toolIn   map[string]any // synthetic tool_call args
	toolRes  map[string]any // synthetic tool_result
}

// decide routes an ask according to the mode.
func (h *Hook) decide(ctx context.Context, s HookSettings, ask Ask) decision {
	switch s.Mode {
	case "all":
		return decision{route: RouteAgent, text: ask.Query}
	case "keyword":
		if t, ok := stripKeyword(Normalize(ask.Query, s.WakeWords, false), s.Keywords); ok {
			return decision{route: RouteAgent, text: t}
		}
	case "music":
		p := ParseMusic(ask.Query, s.WakeWords, s.MusicTriggers, s.MusicExclude)
		if !p.Music {
			break
		}
		if d, ok := h.resolveMusic(ctx, s, p.Content); ok {
			return d
		}
		return decision{route: RouteAgent, text: ask.Query}
	}
	return decision{route: RouteIgnored}
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }

// resolveMusic tries the LLM-free paths: a named artist (random track) or a
// confident, unambiguous search hit.
func (h *Hook) resolveMusic(ctx context.Context, s HookSettings, content string) (decision, bool) {
	m := h.d.Music
	if m == nil || m.URLFor == nil {
		return decision{}, false
	}
	start := time.Now()
	if m.Tracks != nil {
		want := norm(content)
		var cands []media.Track
		artist := ""
		for _, t := range m.Tracks() {
			names := append([]string{t.Artist}, t.Artists...)
			for _, a := range names {
				if a != "" && norm(a) == want {
					cands, artist = append(cands, t), firstNonEmpty(artist, a)
					break
				}
			}
		}
		if len(cands) > 0 {
			t := cands[h.randn(len(cands))]
			return decision{route: RouteArtistRandom, track: t, searchMS: time.Since(start).Milliseconds(),
				reply:    "好的，随机播放" + artist + "的《" + t.Title + "》",
				toolArgs: "artist:" + artist, toolSum: itoa(len(cands)) + " tracks, picked " + t.Title,
				toolIn:  map[string]any{"query": content},
				toolRes: map[string]any{"count": len(cands), "artist": artist, "picked": map[string]any{"id": t.ID, "title": t.Title}}}, true
		}
	}
	if m.Search == nil || m.Lookup == nil {
		return decision{}, false
	}
	minScore := 0
	if m.MinScore != nil {
		minScore = m.MinScore()
	}
	hits, amb, err := m.Search(ctx, content, 5, minScore)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		h.d.Log.Warn("speaker hook: direct search failed", "err", err)
		return decision{}, false
	}
	if len(hits) == 0 || amb || hits[0].Score < float64(s.DirectPlayScore) {
		h.d.Log.Info("speaker hook: no confident hit, using the agent", "content", content, "hits", len(hits), "ambiguous", amb)
		return decision{}, false
	}
	tr, ok := m.Lookup(hits[0].ID)
	if !ok {
		return decision{}, false
	}
	reply := "好的，播放《" + tr.Title + "》"
	if tr.Artist != "" {
		reply = "好的，播放" + tr.Artist + "的《" + tr.Title + "》"
	}
	return decision{route: RouteDirect, track: tr, reply: reply, searchMS: ms, toolArgs: content,
		toolSum: tr.Title + " score=" + strconv.FormatFloat(hits[0].Score, 'f', 0, 64),
		toolIn:  map[string]any{"query": content},
		toolRes: map[string]any{"count": len(hits), "ambiguous": amb, "top": map[string]any{"id": tr.ID, "title": tr.Title, "artist": tr.Artist, "score": hits[0].Score}}}, true
}

func (h *Hook) randn(n int) int {
	if h.Rand != nil {
		return h.Rand(n)
	}
	return rand.IntN(n)
}

func itoa(n int) string { return strconv.Itoa(n) }

func (h *Hook) handle(ctx context.Context, drv Driver, dev Device, s HookSettings, ask Ask) {
	t0 := time.Now()
	// A new ask always ends the running turn.
	h.cancelTurn(dev.DeviceID)
	d := h.decide(ctx, s, ask)
	ok := d.route != RouteIgnored
	var conv string
	var cerr error
	if ok {
		conv, cerr = h.conversation(ctx, dev)
	}
	tid := newTurnID()
	h.mu.Lock()
	h.last = &LastAsk{Device: dev.DeviceID, Query: ask.Query, Time: ask.Time, Handled: ok, Route: d.route}
	h.mu.Unlock()
	h.d.Mgr.NoteAskSeen(dev.DeviceID, ask.Query, ask.Time)
	h.d.Mgr.NoteAsk(dev.DeviceID, ask.Query, time.Since(ask.Time), ok, d.route, tid)
	h.d.Log.Info("speaker hook: ask", "device", dev.DeviceID, "query", ask.Query, "handled", ok, "route", d.route)
	if !ok {
		if ask.Native && dev.DeviceID == DebugDeviceID {
			h.d.Mgr.Log().Record(MockEvent{Device: dev.DeviceID, Kind: EvNativeReply, Text: "（小爱原生回复）", TurnID: tid})
		}
		return
	}
	if cerr != nil {
		h.d.Log.Error("speaker hook: conversation", "device", dev.DeviceID, "err", cerr)
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
		if d.route == RouteAgent {
			h.runTurn(tctx, drv, dev, s, conv, d.text, ask, t0, tid)
		} else {
			h.runDirect(tctx, drv, dev, s, conv, ask, d, t0, tid)
		}
	}()
}

// runDirect answers a music request without the LLM: TTS confirmation, then
// (after it finished) PlayByURL; the exchange is stored in the conversation.
func (h *Hook) runDirect(ctx context.Context, drv Driver, dev Device, s HookSettings, conv string, ask Ask, d decision, t0 time.Time, tid string) {
	ctx = WithTurnID(ctx, tid)
	sp := &spans{h: h, id: tid, conv: conv, query: ask.Query, t0: t0, marks: map[string]int64{}}
	sp.markMS("ask_detected", max(0, min(time.Since(ask.Time).Milliseconds(), 60000)), store.TraceOK)
	status, errMsg := store.TraceOK, ""
	h.flow(dev.DeviceID, tid, EvTurnStart, d.route, map[string]any{"route": d.route, "text": ask.Query})
	callID := "call_" + tid
	h.flow(dev.DeviceID, tid, EvToolCall, "", map[string]any{"name": "search_audio", "args": d.toolIn, "call_id": callID + "_s"})
	h.flow(dev.DeviceID, tid, EvToolResult, "", map[string]any{"call_id": callID + "_s", "name": "search_audio", "ok": true,
		"duration_ms": d.searchMS, "result": d.toolRes})
	defer func() {
		h.flow(dev.DeviceID, tid, EvTurnEnd, "", map[string]any{"duration_ms": time.Since(t0).Milliseconds(), "canceled": ctx.Err() != nil})
		if ctx.Err() != nil {
			status = store.TraceCanceled
		}
		if tr := h.d.Tracer; tr != nil {
			tr.Trace(store.TraceRow{TS: t0, ConversationID: conv, TurnID: sp.id, Kind: store.TraceTurn, Name: "turn", Status: status,
				DurationMS: time.Since(t0).Milliseconds(), Args: ask.Query, ResultSummary: d.reply, Error: errMsg,
				FinishReason: d.route, Channel: store.ChannelSpeaker})
		}
	}()
	if tr := h.d.Tracer; tr != nil {
		tr.Trace(store.TraceRow{TS: t0, ConversationID: conv, TurnID: sp.id, Step: 1, Kind: store.TraceTool, Name: "search_audio",
			Status: store.TraceOK, DurationMS: d.searchMS, Args: d.toolArgs, ResultSummary: d.toolSum, Channel: store.ChannelSpeaker})
	}

	stop := make(chan struct{})
	close(stop) // the reply is ready: just pause once, no recheck window
	intDone := make(chan struct{})
	go func() {
		defer close(intDone)
		h.interrupt(ctx, drv, dev, s.InterruptWindow, stop)
		sp.mark("interrupt", t0, store.TraceOK)
	}()
	select {
	case <-intDone:
	case <-ctx.Done():
		return
	}
	if err := drv.TextToSpeech(ctx, dev, d.reply); err != nil {
		if ctx.Err() == nil {
			errMsg, status = err.Error(), store.TraceError
			h.d.Log.Warn("speaker hook: tts failed", "device", dev.DeviceID, "err", err)
		}
	} else {
		sp.mark("first_tts", t0, store.TraceOK)
	}
	h.d.Mgr.NoteReply(dev.DeviceID, d.reply, sp.id)
	if h.d.Store != nil {
		bg := context.WithoutCancel(ctx)
		for _, m := range []store.Message{{Role: "user", Content: ask.Query}, {Role: "assistant", Content: d.reply}} {
			if _, err := h.d.Store.AppendMessage(bg, conv, m); err != nil {
				h.d.Log.Warn("speaker hook: persist message", "err", err)
			}
		}
	}
	if !h.waitFinish(ctx, drv, dev, d.reply) {
		return
	}
	u, err := h.d.Music.URLFor(d.track)
	if err == nil {
		start := time.Now()
		h.flow(dev.DeviceID, tid, EvToolCall, "", map[string]any{"name": "play_audio", "args": map[string]any{"id": d.track.ID}, "call_id": callID + "_p"})
		err = drv.PlayByURL(WithPlayMeta(ctx, d.track.Title, d.track.Artist), dev, u)
		res := map[string]any{"call_id": callID + "_p", "name": "play_audio", "ok": err == nil, "duration_ms": time.Since(start).Milliseconds()}
		if err != nil {
			res["error"] = err.Error()
		} else {
			res["result"] = map[string]any{"status": "playing", "title": d.track.Title, "artist": d.track.Artist}
		}
		h.flow(dev.DeviceID, tid, EvToolResult, "", res)
		if tr := h.d.Tracer; tr != nil {
			row := store.TraceRow{TS: start, ConversationID: conv, TurnID: sp.id, Step: 2, Kind: store.TraceTool, Name: "play_audio",
				Status: store.TraceOK, DurationMS: time.Since(start).Milliseconds(), Args: d.track.ID, ResultSummary: "played", Channel: store.ChannelSpeaker}
			if err != nil {
				row.Status, row.Error = store.TraceError, err.Error()
			}
			tr.Trace(row)
		}
	}
	if err != nil && ctx.Err() == nil {
		errMsg, status = err.Error(), store.TraceError
		h.d.Log.Warn("speaker hook: direct play failed", "device", dev.DeviceID, "err", err)
	}
	h.d.Log.Info("speaker turn", "device", dev.DeviceID, "turn_id", sp.id, "route", d.route, "search_ms", d.searchMS, "total_ms", time.Since(t0).Milliseconds())
}

// flow records a timeline event of a hook turn.
func (h *Hook) flow(dev, tid, kind, route string, data map[string]any) {
	h.d.Mgr.Log().Record(MockEvent{Device: dev, Kind: kind, TurnID: tid, Route: route, Data: data})
}

const resultMax = 2048

// resultData renders a tool result (truncated to resultMax bytes).
func resultData(raw []byte) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	if len(raw) <= resultMax && json.Valid(raw) {
		return json.RawMessage(raw), false
	}
	return store.TruncateText(string(raw), resultMax), true
}

func newTurnID() string {
	var rb [4]byte
	_, _ = crand.Read(rb[:])
	return "spk-" + hex.EncodeToString(rb[:])
}

// LookupConversation returns the device's existing speaker conversation id
// ("" if none yet); it never creates one.
func (h *Hook) LookupConversation(deviceID string) string {
	h.mu.Lock()
	id := h.convs[deviceID]
	h.mu.Unlock()
	if id != "" || h.d.Store == nil {
		return id
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := h.d.Store.GetSetting(ctx, "speaker.conversation."+deviceID)
	if err != nil || v == "" {
		return ""
	}
	if _, err := h.d.Store.GetConversation(ctx, v); err != nil {
		return ""
	}
	h.mu.Lock()
	h.convs[deviceID] = v
	h.mu.Unlock()
	return v
}

// Conversation returns the device's speaker conversation, creating it.
func (h *Hook) Conversation(ctx context.Context, dev Device) (string, error) {
	return h.conversation(ctx, dev)
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

func (h *Hook) runTurn(ctx context.Context, drv Driver, dev Device, s HookSettings, conv, text string, ask Ask, t0 time.Time, tid string) {
	ctx = WithTurnID(ctx, tid)
	sp := &spans{h: h, id: tid, conv: conv, query: text, t0: t0, marks: map[string]int64{}}
	// Detection lag since the device recorded the ask (clock skew clamped).
	sp.markMS("ask_detected", max(0, min(time.Since(ask.Time).Milliseconds(), 60000)), store.TraceOK)
	h.flow(dev.DeviceID, tid, EvTurnStart, RouteAgent, map[string]any{"route": RouteAgent, "text": text})
	defer func() {
		h.flow(dev.DeviceID, tid, EvTurnEnd, "", map[string]any{"duration_ms": time.Since(t0).Milliseconds(), "canceled": ctx.Err() != nil})
	}()

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
		callStart := map[string]time.Time{}
		obs := &agent.Observer{
			LLMStart: func(step int, model string) {
				h.flow(dev.DeviceID, tid, EvLLMStart, "", map[string]any{"model": model, "step": step})
			},
			LLMEnd: func(step int, row store.TraceRow) {
				d := map[string]any{"step": step, "latency_ms": row.FirstTokenMS, "duration_ms": row.DurationMS}
				if row.PromptTokens+row.CompletionTokens > 0 {
					d["tokens"] = map[string]any{"prompt": row.PromptTokens, "completion": row.CompletionTokens}
				}
				if row.Error != "" {
					d["error"] = row.Error
				}
				h.flow(dev.DeviceID, tid, EvLLMEnd, "", d)
			},
		}
		in := agent.Input{ConversationID: conv, Channel: store.ChannelSpeaker, Text: text, Observer: obs}
		err := h.d.Agent.Run(WithPlayQueue(ctx, pq), in, func(ev agent.Event) {
			switch ev.Type {
			case agent.EventToolCall:
				if tc, ok := ev.Data.(agent.ToolCallData); ok {
					callStart[tc.ID] = time.Now()
					var args any = map[string]any{}
					if len(tc.Arguments) > 0 && json.Valid(tc.Arguments) {
						args = json.RawMessage(tc.Arguments)
					}
					h.flow(dev.DeviceID, tid, EvToolCall, "", map[string]any{"name": tc.Name, "args": args, "call_id": tc.ID})
				}
			case agent.EventToolResult:
				if tr, ok := ev.Data.(agent.ToolResultData); ok {
					d := map[string]any{"call_id": tr.ID, "name": tr.Name, "ok": tr.Error == "",
						"duration_ms": time.Since(callStart[tr.ID]).Milliseconds()}
					if v, trunc := resultData(tr.Result); v != nil {
						d["result"] = v
						if trunc {
							d["truncated"] = true
						}
					}
					if tr.Error != "" {
						d["error"] = tr.Error
					}
					h.flow(dev.DeviceID, tid, EvToolResult, "", d)
				}
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
		h.d.Mgr.NoteReply(dev.DeviceID, reply.String(), sp.id)
	}
	// A queued play_audio starts only after the last sentence has been spoken.
	if req := pq.Take(); req != nil && ctx.Err() == nil {
		select {
		case <-intDone:
		case <-ctx.Done():
			return
		}
		if prev == "" || h.waitFinish(ctx, drv, dev, prev) {
			if err := drv.PlayByURL(WithPlayMeta(ctx, req.Title, req.Artist), dev, req.URL); err != nil && ctx.Err() == nil {
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
