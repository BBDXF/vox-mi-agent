package xiaomibridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/buildinfo"
)

// DefaultListen is the API address: all interfaces, port 4390.
const DefaultListen = ":4390"

// Options wire a bridge; zero values mean the on-speaker defaults.
type Options struct {
	ConfigPath    string        // default DefaultConfigPath
	LogPath       string        // instruction log; default DefaultInstructionLog
	Poll          time.Duration // log poll interval; default 10ms
	DefaultDevice string        // device name until configured; default hostname
	Dev           *Device       // default NewDevice()
	HTTP          *http.Client  // push client
	Log           *slog.Logger  // default slog.Default()
	Now           func() time.Time
}

// Bridge follows the instruction log, publishes recognized speech (push URL,
// SSE stream, MCP) and serves the control API.
type Bridge struct {
	opts    Options
	store   *Store
	dev     *Device
	hub     *Hub
	push    *Pusher
	log     *slog.Logger
	started time.Time

	lines    atomic.Int64
	lastLine atomic.Int64 // unix ms

	mcpMu    sync.Mutex // guards mcpTools (built on first /v1/mcp.json)
	mcpTools []toolInfo
}

// New loads the config file and builds a bridge.
func New(o Options) (*Bridge, error) {
	if o.ConfigPath == "" {
		o.ConfigPath = DefaultConfigPath
	}
	if o.LogPath == "" {
		o.LogPath = DefaultInstructionLog
	}
	if o.Poll <= 0 {
		o.Poll = 10 * time.Millisecond
	}
	if o.DefaultDevice == "" {
		o.DefaultDevice, _ = os.Hostname()
		if o.DefaultDevice == "" {
			o.DefaultDevice = "speaker"
		}
	}
	if o.Dev == nil {
		o.Dev = NewDevice()
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	st, err := OpenStore(o.ConfigPath, o.DefaultDevice)
	if err != nil {
		return nil, err
	}
	b := &Bridge{opts: o, store: st, dev: o.Dev, hub: NewHub(), log: o.Log, started: o.Now()}
	b.push = NewPusher(func() PushSettings { return b.store.Get().Push }, o.HTTP, 0, o.Log)
	return b, nil
}

// ListenAndServe listens on addr (DefaultListen if empty) and serves.
func (b *Bridge) ListenAndServe(ctx context.Context, addr string) error {
	if addr == "" {
		addr = DefaultListen
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return b.Serve(ctx, ln)
}

// Serve runs the log follower, the pusher and the API on ln until ctx is done.
func (b *Bridge) Serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if b.store.View().Auth.DefaultCredentials {
		b.log.Warn("using the default API credentials; change them with PUT /v1/config", "username", DefaultUsername)
	}
	srv := &http.Server{
		Handler:           b.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx }, // ends SSE streams on shutdown
	}
	var wg sync.WaitGroup
	errc := make(chan error, 2)
	wg.Go(func() {
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	})
	wg.Go(func() { b.push.Run(ctx) })
	wg.Go(func() {
		t := &Tailer{Path: b.opts.LogPath, Every: b.opts.Poll}
		if err := t.Run(ctx, b.onLine()); err != nil {
			errc <- err
		}
	})
	b.log.Info("xiaomi-bridge started", "addr", ln.Addr().String(), "config", b.opts.ConfigPath, "instruction_log", b.opts.LogPath)
	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = srv.Shutdown(sctx)
	scancel()
	wg.Wait()
	return err
}

// onLine turns instruction log lines into published events. Consecutive
// identical results (the log can repeat them) are published once.
func (b *Bridge) onLine() func([]byte) {
	var last Recognition
	return func(line []byte) {
		b.lines.Add(1)
		b.lastLine.Store(b.opts.Now().UnixMilli())
		rec, ok := ParseRecognition(line)
		if !ok || rec == last {
			return
		}
		last = rec
		ev := b.hub.Publish(Event{Type: "asr", Device: b.store.Get().Device, DialogID: rec.DialogID, Text: rec.Text, Final: rec.Final, TS: b.opts.Now().UnixMilli()})
		if ev.Final {
			b.log.Info("asr", "seq", ev.Seq, "dialog_id", ev.DialogID, "text", ev.Text)
		}
		b.push.Offer(ev)
	}
}

// StatusReport is the internal state served by /v1/status and get_status.
type StatusReport struct {
	Version            string `json:"version"`
	UptimeS            int64  `json:"uptime_s"`
	Device             string `json:"device"`
	DefaultCredentials bool   `json:"default_credentials"`
	InstructionLog     struct {
		Path       string `json:"path"`
		Exists     bool   `json:"exists"`
		Lines      int64  `json:"lines"`                  // read since start
		LastLineAt int64  `json:"last_line_at,omitempty"` // unix ms
	} `json:"instruction_log"`
	ASR struct {
		Latest        *Event `json:"latest"`
		Finals        int64  `json:"finals"`
		StreamClients int    `json:"stream_clients"`
	} `json:"asr"`
	Push struct {
		Enabled bool   `json:"enabled"`
		URL     string `json:"url,omitempty"`
		Partial bool   `json:"partial"`
		PushStats
	} `json:"push"`
	TTS struct {
		Speaking bool   `json:"speaking"`
		Text     string `json:"text,omitempty"`
		Since    int64  `json:"since,omitempty"` // unix ms
	} `json:"tts"`
	Player      string `json:"player,omitempty"` // playing | paused | idle
	PlayerError string `json:"player_error,omitempty"`
}

// Status gathers the internal state; the player query is bounded to 2 s.
func (b *Bridge) Status(ctx context.Context) StatusReport {
	var r StatusReport
	v := b.store.View()
	r.Version = buildinfo.Version + " (" + buildinfo.Commit + ")"
	r.UptimeS = int64(b.opts.Now().Sub(b.started) / time.Second)
	r.Device = v.Device
	r.DefaultCredentials = v.Auth.DefaultCredentials
	r.InstructionLog.Path = b.opts.LogPath
	_, err := os.Stat(b.opts.LogPath)
	r.InstructionLog.Exists = err == nil
	r.InstructionLog.Lines = b.lines.Load()
	r.InstructionLog.LastLineAt = b.lastLine.Load()
	r.ASR.Latest, r.ASR.Finals = b.hub.Latest()
	r.ASR.StreamClients = b.hub.Subscribers()
	r.Push.Enabled = v.Push.URL != ""
	r.Push.URL = v.Push.URL
	r.Push.Partial = v.Push.Partial
	r.Push.PushStats = b.push.Stats()
	if text, since, ok := b.dev.Speaking(); ok {
		r.TTS.Speaking, r.TTS.Text, r.TTS.Since = true, text, since.UnixMilli()
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if st, err := b.dev.PlayerStatus(pctx); err != nil {
		r.PlayerError = err.Error()
	} else {
		r.Player = st
	}
	return r
}
