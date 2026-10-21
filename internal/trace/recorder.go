package trace

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/andy/vox-mi-agent/internal/agent"
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/store"
)

// Recorder implements agent.Tracer on top of the async traces writer and the
// payload log, and runs the hourly retention job.
type Recorder struct {
	st  *store.Store
	log *slog.Logger
	W   *Writer
	P   *PayloadLog
	dir string

	mu       sync.Mutex
	days     int
	maxRows  int
	stop     chan struct{}
	stopped  chan struct{}
	started  bool
	closeOne sync.Once
}

var _ agent.Tracer = (*Recorder)(nil)

// NewRecorder creates the recorder; payload files live in logDir. Call Start
// to launch the retention job and Close on shutdown (before closing st).
func NewRecorder(st *store.Store, logDir string, log *slog.Logger, wo WriterOptions) *Recorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{
		st: st, log: log, dir: logDir,
		W: NewWriter(st, log, wo), P: NewPayloadLog(logDir, log),
		days: 30, maxRows: 100000,
		stop: make(chan struct{}), stopped: make(chan struct{}),
	}
}

// LogDir is the payload directory.
func (r *Recorder) LogDir() string { return r.dir }

// Trace implements agent.Tracer.
func (r *Recorder) Trace(row store.TraceRow) { r.W.Add(row) }

// PayloadEnabled implements agent.Tracer.
func (r *Recorder) PayloadEnabled() bool { return r.P.Enabled() }

// Payload implements agent.Tracer.
func (r *Recorder) Payload(p agent.Payload) { r.P.Write(p) }

// Configure applies live logging settings.
func (r *Recorder) Configure(l config.Logging) {
	r.mu.Lock()
	r.days, r.maxRows = l.TraceRetentionDays, l.TraceMaxRows
	r.mu.Unlock()
	r.P.Configure(l.DebugPayloads, l.PayloadRetentionDays, int64(l.PayloadMaxBytes))
}

// Flush writes everything buffered so far (traces and payloads).
func (r *Recorder) Flush() { r.W.Flush(); r.P.Flush() }

// PruneNow runs one retention pass and returns the deleted trace rows.
func (r *Recorder) PruneNow(ctx context.Context) (int64, error) {
	r.mu.Lock()
	days, maxRows := r.days, r.maxRows
	r.mu.Unlock()
	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().AddDate(0, 0, -days)
	}
	n, err := r.st.PruneTraces(ctx, cutoff, maxRows)
	r.P.Prune()
	return n, err
}

// Start launches the retention job (once at start, then hourly).
func (r *Recorder) Start() {
	r.started = true
	go func() {
		defer close(r.stopped)
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if n, err := r.PruneNow(ctx); err != nil {
				r.log.Warn("trace retention failed", "err", err)
			} else if n > 0 {
				r.log.Info("trace retention", "deleted_rows", n)
			}
		}
		run()
		tk := time.NewTicker(time.Hour)
		defer tk.Stop()
		for {
			select {
			case <-tk.C:
				run()
			case <-r.stop:
				return
			}
		}
	}()
}

// Close stops the retention job and flushes both writers.
func (r *Recorder) Close() {
	r.closeOne.Do(func() {
		close(r.stop)
		if r.started {
			<-r.stopped
		}
		r.W.Close()
		r.P.Close()
	})
}

// PayloadDir joins the data dir with the logs subdirectory.
func PayloadDir(dataDir string) string { return filepath.Join(dataDir, "logs") }
