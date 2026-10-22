package speaker

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/llm"
	"github.com/andy/vox-mi-agent/internal/media"
	"github.com/andy/vox-mi-agent/internal/store"
	"github.com/andy/vox-mi-agent/internal/tools/audio"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func openStore(t *testing.T) *store.Store {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// fakeAgent answers with scripted sentences, optionally after a delay, and
// records its inputs and cancellations.
type fakeAgent struct {
	mu        sync.Mutex
	sentences []string
	delay     time.Duration
	inputs    []agent.Input
	canceled  []string
	canceledN int
}

func (f *fakeAgent) Run(ctx context.Context, in agent.Input, emit func(agent.Event)) error {
	f.mu.Lock()
	f.inputs = append(f.inputs, in)
	s, d := append([]string(nil), f.sentences...), f.delay
	f.mu.Unlock()
	emit(agent.Event{Type: agent.EventConversation, Data: agent.ConversationData{ID: in.ConversationID}})
	select {
	case <-time.After(d):
	case <-ctx.Done():
		f.mu.Lock()
		f.canceledN++
		f.mu.Unlock()
		return ctx.Err()
	}
	emit(agent.Event{Type: agent.EventToken, Data: agent.TokenData{Text: "x"}})
	for _, x := range s {
		emit(agent.Event{Type: agent.EventSentence, Data: agent.SentenceData{Text: x}})
	}
	emit(agent.Event{Type: agent.EventDone, Data: agent.DoneData{}})
	return nil
}

func (f *fakeAgent) CancelConversation(id string) bool {
	f.mu.Lock()
	f.canceled = append(f.canceled, id)
	f.mu.Unlock()
	return true
}

func (f *fakeAgent) Inputs() []agent.Input {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.Input(nil), f.inputs...)
}

type recTracer struct {
	mu   sync.Mutex
	rows []store.TraceRow
}

func (r *recTracer) Trace(row store.TraceRow) {
	r.mu.Lock()
	r.rows = append(r.rows, row)
	r.mu.Unlock()
}
func (r *recTracer) PayloadEnabled() bool  { return false }
func (r *recTracer) Payload(agent.Payload) {}
func (r *recTracer) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n []string
	for _, x := range r.rows {
		n = append(n, x.Name)
	}
	return n
}

type rig struct {
	mgr  *Manager
	mock *Mock
	hook *Hook
	ag   *fakeAgent
	st   *store.Store
	tr   *recTracer
}

func newRig(t *testing.T, ag TurnRunner) *rig {
	t.Helper()
	m := NewMock()
	m.CharsPerSec = 0
	mgr := NewManager(quiet)
	mgr.Set(m, nil, 0)
	st := openStore(t)
	tr := &recTracer{}
	h := NewHook(HookDeps{Mgr: mgr, Agent: ag, Store: st, Tracer: tr, Log: quiet})
	h.RecheckEvery, h.StatusPoll, h.StartGrace = 10*time.Millisecond, 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(h.Stop)
	r := &rig{mgr: mgr, mock: m, hook: h, st: st, tr: tr}
	if fa, ok := ag.(*fakeAgent); ok {
		r.ag = fa
	}
	return r
}

func (r *rig) start(mode string, kws ...string) {
	r.hook.Apply(HookSettings{Mode: mode, Keywords: kws, PollInterval: 5 * time.Millisecond, InterruptWindow: 200 * time.Millisecond})
}

func kinds(ev []MockEvent) []string {
	var k []string
	for _, e := range ev {
		if e.Kind != EvAskDetected {
			k = append(k, e.Kind)
		}
	}
	return k
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func count(ev []MockEvent, kind string) int {
	n := 0
	for _, e := range ev {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestMockDriverBasics(t *testing.T) {
	m := NewMock()
	m.CharsPerSec = 0
	ctx := context.Background()
	devs, _ := m.DeviceList(ctx)
	if len(devs) != 2 {
		t.Fatalf("%v", devs)
	}
	d := devs[0]
	if a, _ := m.GetLatestAsk(ctx, d, time.Time{}); a != nil {
		t.Fatal("no ask yet")
	}
	a1 := m.InjectAsk(d.DeviceID, "hi")
	a2 := m.InjectAsk(d.DeviceID, "again")
	if !a2.Time.After(a1.Time) {
		t.Fatal("ask times must increase")
	}
	got, _ := m.GetLatestAsk(ctx, d, a1.Time)
	if got == nil || got.Query != "again" {
		t.Fatalf("%v", got)
	}
	if got, _ := m.GetLatestAsk(ctx, d, a2.Time); got != nil {
		t.Fatal("dedupe")
	}
	_ = m.TextToSpeech(ctx, d, "x")
	_ = m.PlayByURL(ctx, d, "http://u")
	_ = m.PlayerSetVolume(ctx, d, 40)
	st, _ := m.PlayerGetStatus(ctx, d)
	if st.Status != StatusPlaying || st.Volume != 40 {
		t.Fatalf("%+v", st)
	}
	_ = m.PlayerPause(ctx, d)
	if st, _ = m.PlayerGetStatus(ctx, d); st.Status != StatusPaused {
		t.Fatalf("%+v", st)
	}
	_ = m.PlayerPlay(ctx, d)
	_ = m.PlayerStop(ctx, d)
	if st, _ = m.PlayerGetStatus(ctx, d); st.Status != StatusIdle {
		t.Fatalf("%+v", st)
	}
	ev := m.Events(0)
	want := []string{"ask", "ask", "tts", "play_url", "volume", "pause", "play", "stop"}
	if k := kinds(ev); len(k) != len(want) {
		t.Fatalf("%v", k)
	}
	if more := m.Events(ev[3].Seq); len(more) != len(ev)-4 {
		t.Fatalf("after: %d", len(more))
	}
}

func TestMockFaultInjection(t *testing.T) {
	m := NewMock()
	d := Device{DeviceID: "mock-1"}
	m.Configure(0, 1)
	if err := m.TextToSpeech(context.Background(), d, "x"); err != ErrInjected {
		t.Fatalf("%v", err)
	}
	m.Configure(40, 0)
	start := time.Now()
	if err := m.PlayerPause(context.Background(), d); err != nil || time.Since(start) < 35*time.Millisecond {
		t.Fatalf("latency %v %v", time.Since(start), err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := m.PlayerPause(ctx, d); err == nil {
		t.Fatal("latency must honor ctx")
	}
}

func TestManagerPlayerAndDevices(t *testing.T) {
	m := NewMock()
	mgr := NewManager(quiet)
	mgr.Set(m, nil, 0)
	var _ audio.Player = mgr
	if err := mgr.Play(context.Background(), audio.PlayRequest{URL: "http://x/a.mp3"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetVolume(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	_ = mgr.Speak(context.Background(), "hello")
	ev := m.Events(0)
	if len(ev) != 3 || ev[0].Kind != EvPlayURL || ev[0].Device != "mock-1" || ev[0].URL != "http://x/a.mp3" || ev[1].Volume != 30 || ev[2].Text != "hello" {
		t.Fatalf("%+v", ev)
	}
	// Configured devices: first enabled wins; none enabled -> ErrNoDevice.
	mgr.Set(m, []Device{{DeviceID: "a", Enabled: false}, {DeviceID: "b", Enabled: true}}, 0)
	if d, err := mgr.Resolve(""); err != nil || d.DeviceID != "b" {
		t.Fatalf("%v %v", d, err)
	}
	if d, err := mgr.Resolve("a"); err != nil || d.DeviceID != "a" {
		t.Fatalf("%v %v", d, err)
	}
	mgr.Set(m, []Device{{DeviceID: "a"}}, 0)
	if err := mgr.Play(context.Background(), audio.PlayRequest{URL: "u"}); err != ErrNoDevice {
		t.Fatalf("%v", err)
	}
}

func TestStripKeyword(t *testing.T) {
	kws := []string{"请", "你", "问问", "请问"}
	for in, want := range map[string]string{"请播放周杰伦": "播放周杰伦", "请问，今天天气": "今天天气", "问问明天呢": "明天呢", "播放音乐": "", "请": ""} {
		got, ok := stripKeyword(in, kws)
		if got != want || ok != (want != "") {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
}

func TestHookKeywordMode(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"好的。"}}
	r := newRig(t, ag)
	r.start("keyword", "请", "问问")
	r.mock.InjectAsk("mock-1", "播放音乐") // not for us
	time.Sleep(60 * time.Millisecond)
	if len(ag.Inputs()) != 0 || count(r.mock.Events(0), EvPause) != 0 {
		t.Fatal("non-keyword ask must be left to the speaker")
	}
	if la := r.hook.Status().LastAsk; la == nil || la.Handled || la.Query != "播放音乐" || la.Device != "mock-1" {
		t.Fatalf("%+v", la)
	}
	r.mock.InjectAsk("mock-1", "请讲个笑话")
	waitFor(t, "tts", func() bool { return count(r.mock.Events(0), EvTTS) == 1 })
	in := ag.Inputs()
	if len(in) != 1 || in[0].Text != "讲个笑话" || in[0].Channel != "speaker" || in[0].ConversationID == "" {
		t.Fatalf("%+v", in)
	}
	if la := r.hook.Status().LastAsk; !la.Handled {
		t.Fatalf("%+v", la)
	}
}

func TestHookAllAndOffAndStartTime(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"嗯。"}}
	r := newRig(t, ag)
	r.mock.InjectAsk("mock-1", "历史问题") // before start: never handled
	r.start("all")
	r.mock.InjectAsk("mock-1", "随便问问")
	waitFor(t, "tts", func() bool { return count(r.mock.Events(0), EvTTS) == 1 })
	in := ag.Inputs()
	if len(in) != 1 || in[0].Text != "随便问问" {
		t.Fatalf("%+v", in)
	}
	if !r.hook.Status().Running {
		t.Fatal("running")
	}
	// off: stops everything
	r.start("off")
	if r.hook.Status().Running {
		t.Fatal("off must stop")
	}
	r.mock.InjectAsk("mock-1", "没人听")
	time.Sleep(50 * time.Millisecond)
	if len(ag.Inputs()) != 1 {
		t.Fatal("off mode must not listen")
	}
}

func TestHookInterruptSequence(t *testing.T) {
	// The agent is slow, so the interrupt window re-checks the status: the
	// speaker's native answer is paused, then (still playing) paused again,
	// then stopped, all before our first TTS.
	ag := &fakeAgent{sentences: []string{"第一句。", "第二句。"}, delay: 150 * time.Millisecond}
	r := newRig(t, ag)
	r.mock.SetNativeReply(true)
	// A driver whose pause does not stop the native reply immediately.
	slow := &stubborn{Mock: r.mock}
	r.mgr.Set(slow, nil, 0)
	r.start("all")
	r.mock.InjectAsk("mock-1", "你好")
	waitFor(t, "two tts", func() bool { return count(r.mock.Events(0), EvTTS) == 2 })
	k := kinds(r.mock.Events(0))
	if k[0] != "ask" || k[1] != "pause" {
		t.Fatalf("sequence %v", k)
	}
	firstTTS := 0
	for i, x := range r.mock.Events(0) {
		if x.Kind == "tts" {
			firstTTS = i
			break
		}
	}
	pre := k[:firstTTS]
	if count(r.mock.Events(0)[:firstTTS], EvPause) < 2 || count(r.mock.Events(0)[:firstTTS], EvStop) < 1 {
		t.Fatalf("expected pause, pause, stop before the first tts: %v", pre)
	}
	if count(r.mock.Events(0)[firstTTS:], EvPause)+count(r.mock.Events(0)[firstTTS:], EvStop) != 0 {
		t.Fatalf("no interrupts after tts started: %v", k)
	}
	// Spans.
	names := r.tr.names()
	for _, want := range []string{"speaker.ask_detected", "speaker.interrupt", "speaker.llm_first_token", "speaker.first_tts"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Fatalf("missing span %s in %v", want, names)
		}
	}
	for _, row := range r.tr.rows {
		if row.Channel != "speaker" || row.Kind != store.TraceTool {
			t.Fatalf("%+v", row)
		}
	}
}

// stubborn keeps playing after the first pause (late native playback).
type stubborn struct {
	*Mock
	mu     sync.Mutex
	pauses int
}

func (s *stubborn) PlayerPause(ctx context.Context, d Device) error {
	s.mu.Lock()
	s.pauses++
	n := s.pauses
	s.mu.Unlock()
	if n <= 2 {
		_ = s.Mock.PlayerPause(ctx, d)
		s.Mock.mu.Lock()
		s.Mock.playing[d.DeviceID] = time.Time{} // native reply starts late
		delete(s.Mock.paused, d.DeviceID)
		s.Mock.mu.Unlock()
		return nil
	}
	return s.Mock.PlayerPause(ctx, d)
}

func TestHookSequentialSentencesWaitForPlayback(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"一二三四五六七八九十。", "第二句。"}}
	r := newRig(t, ag)
	theMgr = r.mgr
	r.mock.SetCharsPerSec(50) // 10 chars -> ~200ms of playback
	r.start("all")
	r.mock.InjectAsk("mock-1", "q")
	waitFor(t, "first tts", func() bool { return count(r.mock.Events(0), EvTTS) >= 1 })
	t1 := time.Now()
	waitFor(t, "second tts", func() bool { return count(r.mock.Events(0), EvTTS) == 2 })
	if d := time.Since(t1); d < 120*time.Millisecond {
		t.Fatalf("second sentence started %v after the first, before it finished", d)
	}
	waitFor(t, "reply event", func() bool { return count(r.mock.Events(0), EvReply) == 1 })
}

func TestHookNewAskCancelsRunningTurn(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"回答。"}, delay: 300 * time.Millisecond}
	r := newRig(t, ag)
	r.start("all")
	r.mock.InjectAsk("mock-1", "第一个问题")
	waitFor(t, "agent started", func() bool { return len(ag.Inputs()) == 1 })
	ag.mu.Lock()
	ag.delay = 0
	ag.mu.Unlock()
	r.mock.InjectAsk("mock-1", "第二个问题")
	waitFor(t, "second turn answered", func() bool { return len(ag.Inputs()) == 2 && count(r.mock.Events(0), EvTTS) == 1 })
	ag.mu.Lock()
	defer ag.mu.Unlock()
	if ag.canceledN != 1 || len(ag.canceled) != 1 || ag.canceled[0] != ag.inputs[0].ConversationID {
		t.Fatalf("canceled=%d %v", ag.canceledN, ag.canceled)
	}
	if ag.inputs[0].ConversationID != ag.inputs[1].ConversationID {
		t.Fatal("device conversation must be reused")
	}
}

func TestHookConversationPerDevicePersisted(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"好。"}}
	r := newRig(t, ag)
	r.mgr.Set(r.mock, []Device{{DeviceID: "mock-1", Name: "客厅", Enabled: true}, {DeviceID: "mock-2", Name: "卧室", Enabled: true}}, 0)
	r.start("all")
	r.mock.InjectAsk("mock-1", "a")
	r.mock.InjectAsk("mock-2", "b")
	waitFor(t, "both", func() bool { return len(ag.Inputs()) == 2 })
	in := ag.Inputs()
	if in[0].ConversationID == in[1].ConversationID {
		t.Fatal("one conversation per device")
	}
	for _, id := range []string{in[0].ConversationID, in[1].ConversationID} {
		c, err := r.st.GetConversation(context.Background(), id)
		if err != nil || c.Channel != "speaker" || (c.Title != "客厅" && c.Title != "卧室") {
			t.Fatalf("%+v %v", c, err)
		}
	}
	v, err := r.st.GetSetting(context.Background(), "speaker.conversation.mock-1")
	if err != nil || (v != in[0].ConversationID && v != in[1].ConversationID) {
		t.Fatalf("%q %v", v, err)
	}
	// A new hook (restart) reuses the persisted conversation.
	h2 := NewHook(HookDeps{Mgr: r.mgr, Agent: ag, Store: r.st, Log: quiet})
	id, err := h2.conversation(context.Background(), Device{DeviceID: "mock-1", Name: "客厅"})
	if err != nil || id != v {
		t.Fatalf("%q %q %v", id, v, err)
	}
}

func TestHookEndToEndFakeLLM(t *testing.T) {
	st := openStore(t)
	fake := llm.NewFake(llm.FakeTurn{Text: "今天天气很好。适合出去走走。"})
	rt := agent.New(agent.Options{Provider: fake, Store: st, Logger: quiet})
	m := NewMock()
	m.CharsPerSec = 0
	m.SetNativeReply(true)
	mgr := NewManager(quiet)
	mgr.Set(m, nil, 0)
	h := NewHook(HookDeps{Mgr: mgr, Agent: rt, Store: st, Log: quiet})
	h.RecheckEvery, h.StatusPoll, h.StartGrace = 10*time.Millisecond, 5*time.Millisecond, 20*time.Millisecond
	defer h.Stop()
	h.Apply(HookSettings{Mode: "keyword", Keywords: []string{"请"}, PollInterval: 5 * time.Millisecond, InterruptWindow: 100 * time.Millisecond})
	m.InjectAsk("mock-1", "请问今天天气怎么样")
	waitFor(t, "tts x2", func() bool { return count(m.Events(0), EvTTS) == 2 })
	ev := m.Events(0)
	k := kinds(ev)
	if k[0] != "ask" || k[1] != "pause" {
		t.Fatalf("%v", k)
	}
	var tts []string
	for _, e := range ev {
		if e.Kind == EvTTS {
			tts = append(tts, e.Text)
		}
	}
	if tts[0] != "今天天气很好。" || tts[1] != "适合出去走走。" {
		t.Fatalf("%q", tts)
	}
	reqs := fake.Requests()
	last := reqs[0].Messages[len(reqs[0].Messages)-1]
	if last.Content != "问今天天气怎么样" && last.Content != "今天天气怎么样" {
		t.Fatalf("agent saw %q", last.Content)
	}
	waitFor(t, "reply", func() bool { return count(m.Events(0), EvReply) == 1 })
	convs, _ := st.ListConversations(context.Background(), 10, time.Time{})
	if len(convs) != 1 || convs[0].Channel != "speaker" {
		t.Fatalf("%+v", convs)
	}
}

func TestHookApplyIsIdempotent(t *testing.T) {
	r := newRig(t, &fakeAgent{})
	s := HookSettings{Mode: "all", PollInterval: 5 * time.Millisecond}
	r.hook.Apply(s)
	r.hook.mu.Lock()
	c1 := r.hook.cancel
	r.hook.mu.Unlock()
	r.hook.Apply(s)
	r.hook.mu.Lock()
	same := r.hook.cancel != nil && c1 != nil
	r.hook.mu.Unlock()
	if !same || !r.hook.Status().Running {
		t.Fatal("still running")
	}
	// log driver never polls
	r.mgr.Set(NewLog(quiet), nil, 0)
	r.hook.Apply(s)
	if r.hook.Status().Running {
		t.Fatal("log driver must not run the hook")
	}
}

func TestHookDefersPlayAudioUntilReplySpoken(t *testing.T) {
	ag := &playingAgent{sentences: []string{"正在播放。", "请欣赏。"}}
	r := newRig(t, ag)
	theMgr = r.mgr
	r.mock.SetCharsPerSec(50)
	r.start("all")
	r.mock.InjectAsk("mock-1", "放首歌")
	waitFor(t, "play_url", func() bool { return count(r.mock.Events(0), EvPlayURL) == 1 })
	ev := r.mock.Events(0)
	k := kinds(ev)
	want := []string{"ask", "pause", "tts", "tts", "reply", "play_url"}
	if len(k) != len(want) {
		t.Fatalf("%v", k)
	}
	for i := range want {
		if k[i] != want[i] {
			t.Fatalf("%v want %v", k, want)
		}
	}
	// play_url only after the last sentence finished playing (~100ms @50 cps)
	var lastTTS, play time.Time
	for _, e := range ev {
		switch e.Kind {
		case EvTTS:
			lastTTS = e.TS
		case EvPlayURL:
			play = e.TS
		}
	}
	if play.Sub(lastTTS) < 80*time.Millisecond {
		t.Fatalf("play_url %v after last tts", play.Sub(lastTTS))
	}
	if ag.status != "queued" {
		t.Fatalf("tool status %q", ag.status)
	}
	if ev[len(ev)-1].URL != "http://h/song.mp3" {
		t.Fatalf("%+v", ev[len(ev)-1])
	}
}

func TestHookCancelledTurnDropsPendingPlay(t *testing.T) {
	ag := &playingAgent{sentences: []string{"这是一个很长很长的回答内容。"}}
	r := newRig(t, ag)
	theMgr = r.mgr
	r.mock.SetCharsPerSec(10) // ~1.4s of speech
	r.start("all")
	r.mock.InjectAsk("mock-1", "放首歌")
	waitFor(t, "tts", func() bool { return count(r.mock.Events(0), EvTTS) == 1 })
	ag.mu.Lock()
	ag.sentences, ag.noPlay = []string{"好。"}, true
	ag.mu.Unlock()
	r.mock.InjectAsk("mock-1", "算了") // cancels the first turn
	waitFor(t, "second tts", func() bool { return count(r.mock.Events(0), EvTTS) == 2 })
	time.Sleep(300 * time.Millisecond)
	if n := count(r.mock.Events(0), EvPlayURL); n != 0 {
		t.Fatalf("pending play of a cancelled turn must be dropped, got %d play_url", n)
	}
}

func TestManagerPlayImmediateOutsideHook(t *testing.T) {
	m := NewMock()
	mgr := NewManager(quiet)
	mgr.Set(m, nil, 0)
	if err := mgr.Play(context.Background(), audio.PlayRequest{URL: "http://x/a.mp3"}); err != nil {
		t.Fatal(err)
	}
	if count(m.Events(0), EvPlayURL) != 1 {
		t.Fatal("web/CLI play must be immediate")
	}
}

// playingAgent calls the manager's Play like the play_audio tool does.
type playingAgent struct {
	fakeAgent
	status string
	noPlay bool
}

func (p *playingAgent) Run(ctx context.Context, in agent.Input, emit func(agent.Event)) error {
	p.mu.Lock()
	s, noPlay := append([]string(nil), p.sentences...), p.noPlay
	p.mu.Unlock()
	if !noPlay {
		err := mgrFrom(ctx).Play(ctx, audio.PlayRequest{URL: "http://h/song.mp3"})
		p.mu.Lock()
		p.status = "playing"
		if err == audio.ErrDeferred {
			p.status = "queued"
		}
		p.mu.Unlock()
	}
	for _, x := range s {
		emit(agent.Event{Type: agent.EventSentence, Data: agent.SentenceData{Text: x}})
	}
	return nil
}

var theMgr *Manager

func mgrFrom(context.Context) *Manager { return theMgr }

func TestMockEventLatency(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"好。"}}
	r := newRig(t, ag)
	theMgr = r.mgr
	r.start("all")
	r.mock.InjectAsk("mock-1", "q")
	waitFor(t, "reply", func() bool { return count(r.mock.Events(0), EvReply) == 1 })
	var det, pause, tts bool
	for _, e := range r.mock.Events(0) {
		switch e.Kind {
		case EvAsk:
			if e.LatencyMS != nil {
				t.Fatal("ask has no latency")
			}
			continue
		case EvAskDetected:
			det = true
		case EvPause:
			pause = true
		case EvTTS:
			tts = true
		}
		if e.LatencyMS == nil || *e.LatencyMS < 0 {
			t.Fatalf("%s lacks latency_ms: %+v", e.Kind, e)
		}
	}
	if !det || !pause || !tts {
		t.Fatal("missing events")
	}
	evs := r.mock.Events(0)
	var lp, lt int64
	for _, e := range evs {
		if e.Kind == EvPause {
			lp = *e.LatencyMS
		}
		if e.Kind == EvTTS {
			lt = *e.LatencyMS
		}
	}
	if lt < lp {
		t.Fatalf("tts latency %d < pause latency %d", lt, lp)
	}
}

// ---- music routes ----

var wakeDefault = []string{"小爱同学", "小爱"}
var trigDefault = []string{"播放", "放一首", "放首", "放一下", "放", "来一首", "来首", "来点", "我想听", "想听", "听一下", "听听", "听"}
var exclDefault = []string{"放假", "放心", "放松", "听说", "听话", "听见"}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"小爱同学播放晴天":        "播放晴天",
		"小爱同学，播放晴天。":      "播放晴天",
		"小爱，请帮我播放晴天":      "播放晴天",
		"  小爱同学 ！ 麻烦你放首歌": "放首歌",
		"请给我来点音乐":         "来点音乐",
		"播放晴天":            "播放晴天",
		"小爱同学小爱同学放歌":      "放歌",
	} {
		if got := Normalize(in, wakeDefault, true); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	// without polite prefixes (keyword mode) 请 stays
	if got := Normalize("小爱同学，请问问天气", wakeDefault, false); got != "请问问天气" {
		t.Errorf("%q", got)
	}
}

func TestParseMusic(t *testing.T) {
	type c struct {
		in, content string
		music       bool
	}
	for _, x := range []c{
		{"播放一首周杰伦的晴天", "周杰伦的晴天", true},
		{"小爱同学播放晴天", "晴天", true},
		{"小爱同学，我想听周杰伦的歌", "周杰伦", true},
		{"来点陈奕迅的歌", "陈奕迅", true},
		{"放首七里香", "七里香", true},
		{"请帮我放一首稻香吧", "稻香", true},
		{"听听晴天", "晴天", true},
		{"听一下晴天谢谢", "晴天", true},
		{"放假", "", false},
		{"放心吧", "", false},
		{"我听说明天下雨", "", false},
		{"听说周杰伦出新歌了", "", false},
		{"放松一下", "", false},
		{"听话", "", false},
		{"暂停", "", false},
		{"下一首", "", false},
		{"停止播放", "", false},
		{"音量调到30", "", false},
		{"播放", "", false},
		{"放一下", "", false},
		{"小爱同学", "", false},
		{"今天天气怎么样", "", false},
	} {
		p := ParseMusic(x.in, wakeDefault, trigDefault, exclDefault)
		if p.Music != x.music || p.Content != x.content {
			t.Errorf("%q -> %+v want music=%v content=%q", x.in, p, x.music, x.content)
		}
	}
}

type musicRig struct {
	*rig
	tracks   []media.Track
	searches []string
	hits     []media.Hit
	amb      bool
}

func newMusicRig(t *testing.T, ag TurnRunner) *musicRig {
	t.Helper()
	m := &musicRig{rig: newRig(t, ag)}
	m.hook.CharsPerSec = 1000 // the mock never reports "playing": keep the estimate fallback short
	m.tracks = []media.Track{
		{ID: "a1", Title: "晴天", Artist: "周杰伦", Artists: []string{"周杰伦"}},
		{ID: "a2", Title: "七里香", Artist: "周杰伦", Artists: []string{"周杰伦"}},
		{ID: "b1", Title: "十年", Artist: "陈奕迅", Artists: []string{"陈奕迅"}},
	}
	m.hook.d.Music = &MusicDeps{
		Search: func(_ context.Context, q string, _, _ int) ([]media.Hit, bool, error) {
			m.searches = append(m.searches, q)
			return m.hits, m.amb, nil
		},
		Tracks: func() []media.Track { return m.tracks },
		Lookup: func(id string) (media.Track, bool) {
			for _, tr := range m.tracks {
				if tr.ID == id {
					return tr, true
				}
			}
			return media.Track{}, false
		},
		URLFor: func(tr media.Track) (string, error) { return "http://srv/audio/" + tr.ID + ".mp3", nil },
	}
	return m
}

func (m *musicRig) startMusic() {
	m.hook.Apply(HookSettings{Mode: "music", Keywords: []string{"问问"}, WakeWords: wakeDefault, MusicTriggers: trigDefault,
		MusicExclude: exclDefault, DirectPlayScore: 85, PollInterval: 5 * time.Millisecond, InterruptWindow: 100 * time.Millisecond})
}

func detected(ev []MockEvent) *MockEvent {
	for i := range ev {
		if ev[i].Kind == EvAskDetected {
			return &ev[i]
		}
	}
	return nil
}

func TestMusicDirectPlayNoLLM(t *testing.T) {
	st := openStore(t)
	fake := llm.NewFake() // any LLM call would fail: zero scripted turns
	rt := agent.New(agent.Options{Provider: fake, Store: st, Logger: quiet})
	m := newMusicRig(t, rt)
	m.hook.d.Store = st
	m.hook.d.Agent = rt
	m.hits = []media.Hit{{ID: "a1", Title: "晴天", Artist: "周杰伦", Score: 95}}
	m.startMusic()
	m.mock.InjectAsk("mock-1", "小爱同学播放周杰伦的晴天")
	waitFor(t, "play_url", func() bool { return count(m.mock.Events(0), EvPlayURL) == 1 })
	if fake.Calls() != 0 {
		t.Fatalf("LLM must not be called, got %d", fake.Calls())
	}
	ev := m.mock.Events(0)
	want := []string{"ask", "pause", "tts", "reply", "play_url"}
	if k := kinds(ev); len(k) != len(want) || k[0] != want[0] || k[1] != want[1] || k[2] != want[2] || k[3] != want[3] || k[4] != want[4] {
		t.Fatalf("%v", kinds(ev))
	}
	d := detected(ev)
	if d == nil || d.Handled == nil || !*d.Handled || d.Route != RouteDirect || d.LatencyMS == nil {
		t.Fatalf("%+v", d)
	}
	if m.searches[0] != "周杰伦的晴天" {
		t.Fatalf("search content %q", m.searches[0])
	}
	var tts, play MockEvent
	for _, e := range ev {
		if e.Kind == EvTTS {
			tts = e
		}
		if e.Kind == EvPlayURL {
			play = e
		}
	}
	if tts.Text != "好的，播放周杰伦的《晴天》" || !play.TS.After(tts.TS) {
		t.Fatalf("%+v %+v", tts, play)
	}
	if play.URL != "http://srv/audio/a1.mp3" || play.Title != "晴天" || play.Artist != "周杰伦" || play.LatencyMS == nil {
		t.Fatalf("%+v", play)
	}
	if la := m.hook.Status().LastAsk; la == nil || la.Route != RouteDirect || !la.Handled {
		t.Fatalf("%+v", la)
	}
	// conversation + traces
	v, err := st.GetSetting(context.Background(), "speaker.conversation.mock-1")
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.ListMessages(context.Background(), v)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[0].Content != "小爱同学播放周杰伦的晴天" || msgs[1].Role != "assistant" || msgs[1].Content != "好的，播放周杰伦的《晴天》" {
		t.Fatalf("%+v", msgs)
	}
	waitFor(t, "turn trace", func() bool {
		m.tr.mu.Lock()
		defer m.tr.mu.Unlock()
		for _, r := range m.tr.rows {
			if r.Kind == store.TraceTurn {
				return true
			}
		}
		return false
	})
	have := map[string]bool{}
	m.tr.mu.Lock()
	for _, r := range m.tr.rows {
		have[r.Kind+":"+r.Name] = true
	}
	m.tr.mu.Unlock()
	for _, want := range []string{"turn:turn", "tool:search_audio", "tool:play_audio", "tool:speaker.first_tts"} {
		if !have[want] {
			t.Fatalf("missing trace %s in %v", want, have)
		}
	}
}

func TestMusicArtistRandom(t *testing.T) {
	fake := llm.NewFake()
	rt := agent.New(agent.Options{Provider: fake, Store: openStore(t), Logger: quiet})
	m := newMusicRig(t, rt)
	m.hook.d.Store = m.st
	picked := map[string]bool{}
	for i := 0; i < 2; i++ {
		m.hook.Rand = func(n int) int { return i % n }
		m.startMusic()
		m.mock.InjectAsk("mock-1", "来点周杰伦的歌")
		waitFor(t, "play", func() bool { return count(m.mock.Events(0), EvPlayURL) == i+1 })
		ev := m.mock.Events(0)
		for _, e := range ev {
			if e.Kind == EvPlayURL {
				picked[e.Title] = true
			}
		}
		if d := detected(ev); d == nil || d.Route != RouteArtistRandom {
			t.Fatalf("%+v", d)
		}
	}
	if !picked["晴天"] || !picked["七里香"] || picked["十年"] {
		t.Fatalf("picked %v", picked)
	}
	var tts []string
	for _, e := range m.mock.Events(0) {
		if e.Kind == EvTTS {
			tts = append(tts, e.Text)
		}
	}
	if tts[0] != "好的，随机播放周杰伦的《晴天》" {
		t.Fatalf("%q", tts)
	}
	if fake.Calls() != 0 || len(m.searches) != 0 {
		t.Fatal("artist route must not use the LLM or search")
	}
}

func TestMusicFallsBackToAgent(t *testing.T) {
	for name, tc := range map[string]struct {
		hits []media.Hit
		amb  bool
	}{
		"ambiguous": {[]media.Hit{{ID: "a1", Title: "晴天", Artist: "周杰伦", Score: 97}, {ID: "a2", Title: "晴天2", Score: 95}}, true},
		"low score": {[]media.Hit{{ID: "a1", Title: "晴天", Artist: "周杰伦", Score: 70}}, false},
		"no hits":   {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			ag := &fakeAgent{sentences: []string{"我来帮你找找。"}}
			m := newMusicRig(t, ag)
			m.hits, m.amb = tc.hits, tc.amb
			m.startMusic()
			m.mock.InjectAsk("mock-1", "小爱同学播放晴天")
			waitFor(t, "agent speech", func() bool { return count(m.mock.Events(0), EvTTS) == 1 })
			in := ag.Inputs()
			if len(in) != 1 || in[0].Text != "小爱同学播放晴天" { // the original query
				t.Fatalf("%+v", in)
			}
			ev := m.mock.Events(0)
			if d := detected(ev); d == nil || d.Route != RouteAgent || !*d.Handled {
				t.Fatalf("%+v", d)
			}
			if count(ev, EvPlayURL) != 0 {
				t.Fatal("no direct play")
			}
		})
	}
}

func TestMusicModeIgnoresNonMusic(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"x。"}}
	m := newMusicRig(t, ag)
	m.startMusic()
	for _, q := range []string{"暂停", "下一首", "小爱同学今天天气怎么样", "放假安排", "声音大一点"} {
		m.mock.InjectAsk("mock-1", q)
		waitFor(t, "detected", func() bool {
			la := m.hook.Status().LastAsk
			return la != nil && la.Query == q
		})
	}
	time.Sleep(50 * time.Millisecond)
	if len(ag.Inputs()) != 0 || count(m.mock.Events(0), EvPause) != 0 {
		t.Fatal("must be left to the speaker")
	}
	n := 0
	for _, e := range m.mock.Events(0) {
		if e.Kind == EvAskDetected {
			n++
			if *e.Handled || e.Route != RouteIgnored {
				t.Fatalf("%+v", e)
			}
		}
	}
	if n != 5 {
		t.Fatalf("ask_detected events: %d", n)
	}
}

func TestKeywordModeStripsWakeWord(t *testing.T) {
	ag := &fakeAgent{sentences: []string{"好。"}}
	r := newRig(t, ag)
	r.hook.Apply(HookSettings{Mode: "keyword", Keywords: []string{"问问"}, WakeWords: wakeDefault, PollInterval: 5 * time.Millisecond, InterruptWindow: 50 * time.Millisecond})
	r.mock.InjectAsk("mock-1", "小爱同学，问问明天天气")
	waitFor(t, "agent", func() bool { return len(ag.Inputs()) == 1 })
	if in := ag.Inputs(); in[0].Text != "明天天气" {
		t.Fatalf("%+v", in)
	}
}
