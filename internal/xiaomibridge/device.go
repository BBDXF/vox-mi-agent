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
	TTSPlay  string // blocking TTS script: pauses players, synthesizes, then miplayer plays it
	AivsInit string // init script of mico_aivs_lab (XiaoAi's AIVS client)

	mu   sync.Mutex
	play *playback // the bridge's running TTS, if any
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
	cmdTimeout = 5 * time.Second
	ttsTimeout = 10 * time.Minute
	maxOutput  = 2048
)

// playback is one TTS run of the blocking script, in its own process group so
// the script and its miplayer child can be killed together.
type playback struct {
	text    string
	started time.Time
	pgid    int
	done    chan struct{}
	waitErr error
	mu      sync.Mutex
	killed  bool
}

// kill terminates the process group; false if it had already exited.
func (p *playback) kill() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return false // exited: the pgid may be reused
	default:
	}
	p.killed = true
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
	return true
}

func (p *playback) wasKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// Speak plays text, interrupting the bridge's previous TTS. With wait it
// returns when playback ended (interrupted reports an early end by Stop, a
// newer Speak or ctx); otherwise it returns once playback started.
func (d *Device) Speak(ctx context.Context, text string, wait bool) (interrupted bool, err error) {
	cmd := exec.Command(d.TTSPlay, text)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	d.mu.Lock() // serializes concurrent Speak calls: exactly one survives
	if d.play != nil {
		d.play.kill()
	}
	if err := cmd.Start(); err != nil {
		d.play = nil
		d.mu.Unlock()
		return false, fmt.Errorf("%s: %w", filepath.Base(d.TTSPlay), err)
	}
	p := &playback{text: text, started: time.Now(), pgid: cmd.Process.Pid, done: make(chan struct{})}
	d.play = p
	d.mu.Unlock()
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
		d.mu.Lock()
		if d.play == p {
			d.play = nil
		}
		d.mu.Unlock()
	}()
	go func() { // safety net against a hung player
		t := time.NewTimer(ttsTimeout)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			p.kill()
		}
	}()
	if !wait {
		return false, nil
	}
	select {
	case <-p.done:
	case <-ctx.Done():
		p.kill()
		<-p.done
	}
	if p.wasKilled() {
		return true, nil
	}
	if p.waitErr != nil {
		return false, fmt.Errorf("%s: %w", filepath.Base(d.TTSPlay), p.waitErr)
	}
	return false, nil
}

// Speaking returns the text of the bridge's running TTS and when it started.
func (d *Device) Speaking() (text string, since time.Time, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.play == nil {
		return "", time.Time{}, false
	}
	return d.play.text, d.play.started, true
}

// Stop ends playback: it kills the bridge's TTS (script and miplayer child),
// then pauses and stops the system players, which also covers XiaoAi's own
// replies when the firmware honours it. interrupted reports whether the
// bridge's TTS was running.
func (d *Device) Stop(ctx context.Context) (interrupted bool, err error) {
	d.mu.Lock()
	p := d.play
	d.mu.Unlock()
	if p != nil {
		interrupted = p.kill()
	}
	_, e1 := d.run(ctx, d.Mphelper, "pause")
	_, e2 := d.run(ctx, d.Ubus, "call", "mediaplayer", "player_play_operation", ubusArg(map[string]any{"action": "stop"}))
	return interrupted, errors.Join(e1, e2)
}

// PlayURL plays an audio URL on the system media player.
func (d *Device) PlayURL(ctx context.Context, url string) error {
	_, err := d.run(ctx, d.Ubus, "call", "mediaplayer", "player_play_url", ubusArg(map[string]any{"url": url, "type": 1}))
	return err
}

// AbortXiaoAI restarts mico_aivs_lab, dropping XiaoAi's current dialog. It
// takes 1-2 s, native TTS is unavailable meanwhile, and it reportedly cannot
// stop a reply that already started playing on LX06.
func (d *Device) AbortXiaoAI(ctx context.Context) error {
	_, err := d.run(ctx, d.AivsInit, "restart")
	return err
}

// PlayerStatus returns "playing", "paused" or "idle" from mphelper mute_stat.
func (d *Device) PlayerStatus(ctx context.Context) (string, error) {
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
		return s, fmt.Errorf("%s %s: %w: %s", filepath.Base(name), strings.Join(args[:min(len(args), 3)], " "), err, s)
	}
	return s, nil
}

func ubusArg(v map[string]any) string {
	b, _ := json.Marshal(v) // maps of strings/ints always marshal
	return string(b)
}
