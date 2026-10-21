package speaker

import (
	"context"
	"log/slog"
	"time"

	"github.com/andy/vox-mi-agent/internal/tools/audio"
)

// Log is a driver that only logs (and records plays / volumes) every call.
type Log struct {
	Log *slog.Logger
	audio.LogPlayer
}

// NewLog creates a log driver.
func NewLog(l *slog.Logger) *Log {
	if l == nil {
		l = slog.Default()
	}
	return &Log{Log: l}
}

func (l *Log) Name() string { return "log" }

func (l *Log) DeviceList(context.Context) ([]Device, error) {
	return []Device{{DeviceID: "log", Name: "Log speaker", Enabled: true}}, nil
}
func (l *Log) GetLatestAsk(context.Context, Device, time.Time) (*Ask, error) { return nil, nil }
func (l *Log) TextToSpeech(_ context.Context, d Device, text string) error {
	l.Log.Info("speaker(log): tts", "device", d.DeviceID, "text", text)
	return nil
}
func (l *Log) PlayByURL(ctx context.Context, d Device, url string) error {
	l.Log.Info("speaker(log): play", "device", d.DeviceID, "url", url)
	return l.LogPlayer.Play(ctx, audio.PlayRequest{URL: url})
}
func (l *Log) PlayerSetVolume(ctx context.Context, d Device, v int) error {
	l.Log.Info("speaker(log): set volume", "device", d.DeviceID, "volume", v)
	return l.LogPlayer.SetVolume(ctx, v)
}
func (l *Log) PlayerPause(_ context.Context, d Device) error {
	l.Log.Info("speaker(log): pause", "device", d.DeviceID)
	return nil
}
func (l *Log) PlayerPlay(_ context.Context, d Device) error {
	l.Log.Info("speaker(log): play/resume", "device", d.DeviceID)
	return nil
}
func (l *Log) PlayerStop(_ context.Context, d Device) error {
	l.Log.Info("speaker(log): stop", "device", d.DeviceID)
	return nil
}
func (l *Log) PlayerGetStatus(context.Context, Device) (Status, error) { return Status{}, nil }

// StaticDevices returns the single pseudo device.
func (l *Log) StaticDevices() []Device {
	return []Device{{DeviceID: "log", Name: "Log speaker", Enabled: true}}
}
