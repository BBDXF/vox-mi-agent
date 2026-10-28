package xiaomibridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Device runs the speaker's local playback commands. Paths are configurable so
// the bridge can be exercised off-device.
type Device struct {
	Ubus     string // ubus CLI
	Mphelper string // mphelper (pause / mute_stat)
	TTSPlay  string // blocking TTS script: synthesizes to a file, then miplayer plays it
	AivsInit string // init script of mico_aivs_lab (XiaoAi's AIVS client)

	mu   sync.Mutex
	play *managed // the running blocking playback, if any
}

// NewDevice returns a Device with the on-speaker command paths.
func NewDevice() *Device {
	return &Device{
		Ubus:     "ubus",
		Mphelper: "mphelper",
		TTSPlay:  "/usr/sbin/tts_play.sh",
		AivsInit: "/etc/init.d/mico_aivs_lab",
	}
}

const (
	cmdTimeout      = 5 * time.Second
	blockingTimeout = 10 * time.Minute
	maxOutput       = 2048
)

// Speak starts native TTS (mibrain text_to_speech) and returns immediately.
// The playback belongs to XiaoAi's own player, so a new wake-up cuts it off.
func (d *Device) Speak(ctx context.Context, text string) error {
	_, err := d.run(ctx, d.Ubus, "call", "mibrain", "text_to_speech", ubusArg(map[string]any{"text": text, "save": 0}))
	return err
}

// SpeakBlocking plays text with the blocking TTS script and returns when it
// finished. A running blocking playback is stopped first. interrupted reports
// that Stop, a newer blocking playback or ctx ended it early.
func (d *Device) SpeakBlocking(ctx context.Context, text string) (interrupted bool, err error) {
	return d.runManaged(ctx, d.TTSPlay, text)
}

// PlayURL plays an audio URL on the system media player.
func (d *Device) PlayURL(ctx context.Context, url string) error {
	_, err := d.run(ctx, d.Ubus, "call", "mediaplayer", "player_play_url", ubusArg(map[string]any{"url": url, "type": 1}))
	return err
}

// Stop ends local playback: it kills the blocking TTS process group (the
// script and its miplayer child), then pauses and stops the system players.
// interrupted reports whether a blocking playback was running.
func (d *Device) Stop(ctx context.Context) (interrupted bool, err error) {
	d.mu.Lock()
	m := d.play
	d.mu.Unlock()
	if m != nil {
		interrupted = m.kill()
	}
	_, e1 := d.run(ctx, d.Mphelper, "pause")
	_, e2 := d.run(ctx, d.Ubus, "call", "mediaplayer", "player_play_operation", ubusArg(map[string]any{"action": "stop"}))
	return interrupted, errors.Join(e1, e2)
}

// AbortXiaoAI restarts mico_aivs_lab, dropping XiaoAi's current dialog. It
// takes 1-2 s, native TTS is unavailable meanwhile, and it reportedly cannot
// stop a reply that already started playing on LX06.
func (d *Device) AbortXiaoAI(ctx context.Context) error {
	_, err := d.run(ctx, d.AivsInit, "restart")
	return err
}

// Status returns "playing", "paused" or "idle" from mphelper mute_stat.
func (d *Device) Status(ctx context.Context) (string, error) {
	out, err := d.run(ctx, d.Mphelper, "mute_stat")
	if err != nil {
		return "", err
	}
	switch {
	case strings.Contains(out, "1"):
		return "playing", nil
	case strings.Contains(out, "2"):
		return "paused", nil
	}
	return "idle", nil
}

// run executes a short command without a shell (arguments are never parsed).
func (d *Device) run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if len(s) > maxOutput {
		s = s[:maxOutput]
	}
	if err != nil {
		return s, fmt.Errorf("%s %s: %w: %s", filepath.Base(name), firstArg(args), err, s)
	}
	return s, nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args[:min(len(args), 3)], " ")
}

func ubusArg(v map[string]any) string {
	b, _ := json.Marshal(v) // maps of strings/ints always marshal
	return string(b)
}

// managed is a blocking playback running in its own process group.
type managed struct {
	pgid    int
	done    chan struct{}
	waitErr error
	mu      sync.Mutex
	killed  bool
}

// kill terminates the whole process group; false if it had already exited.
func (m *managed) kill() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.done:
		return false // exited: the pgid may be reused
	default:
	}
	m.killed = true
	_ = syscall.Kill(-m.pgid, syscall.SIGKILL)
	return true
}

func (d *Device) runManaged(ctx context.Context, name string, args ...string) (bool, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	m := &managed{pgid: cmd.Process.Pid, done: make(chan struct{})}
	go func() {
		m.waitErr = cmd.Wait()
		close(m.done)
	}()
	d.mu.Lock()
	prev := d.play
	d.play = m
	d.mu.Unlock()
	if prev != nil {
		prev.kill()
	}
	timer := time.NewTimer(blockingTimeout)
	defer timer.Stop()
	select {
	case <-m.done:
	case <-ctx.Done():
		m.kill()
		<-m.done
	case <-timer.C:
		m.kill()
		<-m.done
	}
	d.mu.Lock()
	if d.play == m {
		d.play = nil
	}
	d.mu.Unlock()
	m.mu.Lock()
	killed := m.killed
	m.mu.Unlock()
	if killed {
		return true, nil
	}
	if m.waitErr != nil {
		return false, fmt.Errorf("%s: %w", filepath.Base(name), m.waitErr)
	}
	return false, nil
}
