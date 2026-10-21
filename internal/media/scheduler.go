package media

import (
	"context"
	"sync"
	"time"
)

// Trigger is what the Scheduler needs from a Syncer.
type Trigger interface {
	Trigger(ctx context.Context, source string) (Status, error)
}

// Scheduler runs the sync on start (if configured) and then periodically.
type Scheduler struct {
	T        Trigger
	Interval time.Duration // 0 = manual only
	OnStart  bool
	wg       sync.WaitGroup
}

// NewScheduler wires a Syncer with its config.
func NewScheduler(s *Syncer) *Scheduler {
	return &Scheduler{T: s, Interval: s.cfg.Media.SyncInterval, OnStart: s.cfg.Media.SyncOnStart && len(s.cfg.Media.Folders) > 0}
}

// Start launches the background loop and returns immediately; it stops when
// ctx is cancelled. With Interval 0 and OnStart false it does nothing.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if s.OnStart && ctx.Err() == nil {
			_, _ = s.T.Trigger(ctx, "start")
		}
		if s.Interval <= 0 {
			return
		}
		tk := time.NewTicker(s.Interval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				_, _ = s.T.Trigger(ctx, "timer")
			}
		}
	}()
}

// Wait blocks until the loop has exited (after ctx cancellation).
func (s *Scheduler) Wait() { s.wg.Wait() }
