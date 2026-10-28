package xiaomibridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config wires a bridge. At least one of PushURL and Listen must be set.
type Config struct {
	LogPath   string        // instruction log; default DefaultInstructionLog
	Poll      time.Duration // log poll interval; default 10ms
	Device    string        // device name sent with every event
	PushURL   string        // where recognized speech is POSTed; "" = no push
	PushToken string
	Partial   bool // also push non-final (in-progress) recognition results

	Listen string // control API address, e.g. "0.0.0.0:4399"; "" = disabled
	Token  string // control API bearer token; required with Listen

	Dev  *Device      // default NewDevice()
	HTTP *http.Client // push client; default http.DefaultClient
	Log  *slog.Logger // default slog.Default()
	Now  func() time.Time
}

// Run runs the bridge until ctx is done.
func Run(ctx context.Context, c Config) error {
	if c.PushURL == "" && c.Listen == "" {
		return errors.New("nothing to do: set a push URL and/or a control listen address")
	}
	if c.Listen != "" && c.Token == "" {
		return errors.New("control API requires a token")
	}
	if c.LogPath == "" {
		c.LogPath = DefaultInstructionLog
	}
	if c.Dev == nil {
		c.Dev = NewDevice()
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	errc := make(chan error, 2)

	if c.Listen != "" {
		ln, err := net.Listen("tcp", c.Listen)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: ControlHandler(c.Dev, c.Token, c.Log), ReadHeaderTimeout: 5 * time.Second}
		wg.Go(func() {
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		})
		wg.Go(func() {
			<-ctx.Done()
			sctx, scancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer scancel()
			_ = srv.Shutdown(sctx)
		})
		c.Log.Info("control API listening", "addr", ln.Addr().String())
	}

	if c.PushURL != "" {
		p := NewPusher(c.PushURL, c.PushToken, c.HTTP, 0, c.Log)
		wg.Go(func() { p.Run(ctx) })
		f := newFilter(c.Partial)
		t := &Tailer{Path: c.LogPath, Every: c.Poll}
		wg.Go(func() {
			err := t.Run(ctx, func(line []byte) {
				rec, ok := ParseRecognition(line)
				if !ok || !f.accept(rec) {
					return
				}
				c.Log.Info("asr", "dialog_id", rec.DialogID, "final", rec.Final, "text", rec.Text)
				p.Enqueue(Event{Type: "asr", Device: c.Device, DialogID: rec.DialogID, Text: rec.Text, Final: rec.Final, TS: c.Now().UnixMilli()})
			})
			if err != nil {
				errc <- err
			}
		})
		c.Log.Info("following instruction log", "path", c.LogPath, "push", c.PushURL, "partial", c.Partial)
	}

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	cancel()
	wg.Wait()
	return err
}

// filter drops partials (unless wanted) and repeats of the same result.
type filter struct {
	partial bool
	last    Recognition
}

func newFilter(partial bool) *filter { return &filter{partial: partial} }

func (f *filter) accept(r Recognition) bool {
	if !r.Final && !f.partial {
		return false
	}
	if r == f.last {
		return false
	}
	f.last = r
	return true
}
