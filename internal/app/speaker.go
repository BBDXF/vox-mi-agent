package app

import (
	"github.com/andy/vox-mi-agent/internal/config"
	"github.com/andy/vox-mi-agent/internal/speaker"
	"github.com/andy/vox-mi-agent/internal/speaker/xiaomi"
)

// Xiaomi returns the Xiaomi driver (created on first use, kept across driver
// switches so the login survives).
func (a *App) Xiaomi() *xiaomi.Driver {
	a.xmMu.Lock()
	defer a.xmMu.Unlock()
	if a.xm == nil {
		o := xiaomi.Options{DataDir: a.Cfg.DataDir, Log: a.Log}
		if a.xmOpts != nil {
			a.xmOpts(&o)
		}
		a.xm = xiaomi.NewDriver(o)
	}
	return a.xm
}

func speakerDevices(c *config.Config) []speaker.Device {
	out := make([]speaker.Device, 0, len(c.Speaker.Xiaomi.Devices))
	for _, d := range c.Speaker.Xiaomi.Devices {
		out = append(out, speaker.Device{DeviceID: d.DeviceID, Name: d.Name, Hardware: d.Hardware, Enabled: d.Enabled,
			PlayMode: d.PlayMode, TTSMode: d.TTSMode, MiotTTS: d.MiotTTS})
	}
	return out
}

// applySpeaker makes the speaker settings of c live (driver hot-swap, devices,
// hook). Safe to call at any time.
func (a *App) applySpeaker(c *config.Config) {
	a.Mock.Configure(c.Speaker.Mock.LatencyMS, c.Speaker.Mock.FailRate)
	var drv speaker.Driver
	switch c.Speaker.Driver {
	case "xiaomi":
		drv = a.Xiaomi()
	default:
		drv = a.Mock
	}
	a.Speaker.SetDebug(a.Mock, c.Speaker.DebugDevice)
	a.Speaker.Set(drv, speakerDevices(c), c.Speaker.VolumeDefault)
	a.applyHook(c)
}

func (a *App) applyHook(c *config.Config) {
	a.xmMu.Lock()
	on := a.hookOn
	a.xmMu.Unlock()
	if !on || a.Hook == nil {
		return
	}
	h := c.Speaker.Hook
	a.Hook.Apply(speaker.HookSettings{Mode: h.Mode, Keywords: h.Keywords, WakeWords: h.WakeWords, MusicTriggers: h.MusicTriggers,
		MusicExclude: h.MusicExclude, DirectPlayScore: h.DirectPlayScore, PollInterval: h.PollInterval, InterruptWindow: h.InterruptWindow})
}

// StartSpeaker starts the conversation hook (serve only).
func (a *App) StartSpeaker() {
	a.xmMu.Lock()
	a.hookOn = true
	a.xmMu.Unlock()
	a.applyHook(a.Config())
}
