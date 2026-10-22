// Package trace persists agent trace records: the traces table (async batch
// writer + retention) and the optional debug payload log files.
package trace

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/store"
)

// Sink stores trace rows (implemented by *store.Store).
type Sink interface {
	InsertTraces(ctx context.Context, rows []store.TraceRow) error
}

// Writer buffers trace rows and inserts them in batches from a single
// goroutine. Add never blocks: when the buffer is full the row is dropped,
// counted and warned about.
type Writer struct {
	sink  Sink
	log   *slog.Logger
	ch    chan store.TraceRow
	flush chan chan struct{}
	quit  chan struct{}
	done  chan struct{}

	batch    int
	interval time.Duration

	closed   atomic.Bool
	dropped  atomic.Int64
	written  atomic.Int64
	lastWarn atomic.Int64 // unix nanos of the last drop warning
}

// WriterOptions tunes a Writer; zero values select the defaults
// (buffer 1024, batch 100, flush every 200ms).
type WriterOptions struct {
	Buffer   int
	Batch    int
	Interval time.Duration
}

// NewWriter starts the writer goroutine.
func NewWriter(sink Sink, log *slog.Logger, o WriterOptions) *Writer {
	if o.Buffer <= 0 {
		o.Buffer = 1024
	}
	if o.Batch <= 0 {
		o.Batch = 100
	}
	if o.Interval <= 0 {
		o.Interval = 200 * time.Millisecond
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	w := &Writer{
		sink: sink, log: log, ch: make(chan store.TraceRow, o.Buffer),
		flush: make(chan chan struct{}), quit: make(chan struct{}), done: make(chan struct{}),
		batch: o.Batch, interval: o.Interval,
	}
	go w.loop()
	return w
}

// Add queues a row; it reports false when the row was dropped.
func (w *Writer) Add(r store.TraceRow) bool {
	if w.closed.Load() {
		w.drop()
		return false
	}
	select {
	case w.ch <- r:
		return true
	default:
		w.drop()
		return false
	}
}

func (w *Writer) drop() {
	n := w.dropped.Add(1)
	now := time.Now().UnixNano()
	last := w.lastWarn.Load()
	if n == 1 || now-last > int64(10*time.Second) {
		if w.lastWarn.CompareAndSwap(last, now) {
			w.log.Warn("trace buffer full or closed: dropping trace rows", "dropped_total", n)
		}
	}
}

// Dropped is the number of rows dropped so far.
func (w *Writer) Dropped() int64 { return w.dropped.Load() }

// Written is the number of rows successfully inserted so far.
func (w *Writer) Written() int64 { return w.written.Load() }

// Flush blocks until every row queued before the call has been written.
func (w *Writer) Flush() {
	reply := make(chan struct{})
	select {
	case w.flush <- reply:
		<-reply
	case <-w.done:
	}
}

// Close flushes remaining rows and stops the goroutine. It is idempotent.
func (w *Writer) Close() {
	if w.closed.CompareAndSwap(false, true) {
		close(w.quit)
	}
	<-w.done
}

func (w *Writer) loop() {
	defer close(w.done)
	tk := time.NewTicker(w.interval)
	defer tk.Stop()
	buf := make([]store.TraceRow, 0, w.batch)
	write := func() {
		if len(buf) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := w.sink.InsertTraces(ctx, buf)
		cancel()
		if err != nil {
			w.log.Warn("trace insert failed", "err", err, "rows", len(buf))
		} else {
			w.written.Add(int64(len(buf)))
		}
		buf = buf[:0]
	}
	drain := func() {
		for {
			select {
			case r := <-w.ch:
				buf = append(buf, r)
				if len(buf) >= w.batch {
					write()
				}
			default:
				write()
				return
			}
		}
	}
	for {
		select {
		case r := <-w.ch:
			buf = append(buf, r)
			if len(buf) >= w.batch {
				write()
			}
		case <-tk.C:
			write()
		case reply := <-w.flush:
			drain()
			close(reply)
		case <-w.quit:
			drain()
			return
		}
	}
}
