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
// The interval can be changed while running with SetInterval.
type Scheduler struct {
	T        Trigger
	Interval time.Duration // 0 = manual only
	OnStart  bool
	wg       sync.WaitGroup

	mu       sync.Mutex
	ctx      context.Context // parent ctx given to Start; nil before Start
	stopLoop context.CancelFunc
}

// NewScheduler wires a Syncer with its config.
func NewScheduler(s *Syncer) *Scheduler {
	m := s.config().Media
	return &Scheduler{T: s, Interval: m.SyncInterval, OnStart: m.SyncOnStart && len(m.Folders) > 0}
}

// Start launches the background loop and returns immediately; it stops when
// ctx is cancelled. With Interval 0 and OnStart false it does nothing.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.launchLocked(s.OnStart)
	s.mu.Unlock()
}

// SetInterval changes the period and reschedules the timer (an in-flight sync
// is not interrupted). Before Start it only records the value. 0 = manual.
func (s *Scheduler) SetInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Interval = d
	if s.ctx == nil {
		return
	}
	s.launchLocked(false)
}

// launchLocked (re)starts the loop goroutine, cancelling the previous one.
func (s *Scheduler) launchLocked(onStart bool) {
	if s.stopLoop != nil {
		s.stopLoop()
	}
	parent, interval := s.ctx, s.Interval
	loopCtx, cancel := context.WithCancel(parent)
	s.stopLoop = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Syncs run under the parent ctx so that rescheduling never aborts one.
		if onStart && parent.Err() == nil {
			_, _ = s.T.Trigger(parent, "start")
		}
		if interval <= 0 {
			return
		}
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-tk.C:
				if loopCtx.Err() != nil { // rescheduled meanwhile
					return
				}
				_, _ = s.T.Trigger(parent, "timer")
			}
		}
	}()
}

// Wait blocks until the loop has exited (after ctx cancellation).
func (s *Scheduler) Wait() { s.wg.Wait() }
