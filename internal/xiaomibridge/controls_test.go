package xiaomibridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/speaker"
	bridgeclient "github.com/BBDXF/vox-mi-agent/internal/speaker/bridge"
)

func controlBridge(t *testing.T) (*Bridge, *bridgeclient.Driver, speaker.Device, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := New(Options{ConfigPath: filepath.Join(dir, "config.json"), LogPath: filepath.Join(dir, "instruction.log"), Dev: fakeDevice(t, dir), Log: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(b.Handler())
	t.Cleanup(s.Close)
	t.Cleanup(func() { b.dev.Stop(context.Background()) })
	dev := speaker.Device{DeviceID: "local-id", Enabled: true}
	d := bridgeclient.NewDriver([]bridgeclient.Endpoint{{Device: dev, URL: s.URL, Username: DefaultUsername, Password: DefaultPassword}})
	return b, d, dev, dir
}

func script(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalPauseTerminatesTTSAndStatusTracksSpeech(t *testing.T) {
	b, d, dev, dir := controlBridge(t)
	ctx := context.Background()
	if err := d.TextToSpeech(ctx, dev, "local speech"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := b.dev.Speaking(); !ok {
		t.Fatal("TTS did not start asynchronously")
	}
	// TTS must override a firmware player that currently reports idle.
	b.dev.Mphelper = script(t, dir, "idle-mphelper", `[ "$1" = mute_stat ] && echo 0; exit 0`)
	st, err := d.PlayerGetStatus(ctx, dev)
	if err != nil || st.Status != speaker.StatusPlaying {
		t.Fatalf("TTS status = %+v, %v", st, err)
	}
	if err := d.PlayerPause(ctx, dev); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pause terminates TTS process group", func() bool { _, _, ok := b.dev.Speaking(); return !ok })
}

func TestControlEndpointAuthAndVolumeValidation(t *testing.T) {
	b, _, _, _ := controlBridge(t)
	for _, path := range []string{"/v1/pause", "/v1/play", "/v1/set-volume"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"volume":20}`))
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated: %d", path, w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"volume":null}`, `{"volume":-1}`, `{"volume":101}`, `{"volume":1.5}`, `{"volume":20,"unexpected":true}`, `{"volume":20}{}`, `{"volume":20} trailing`} {
		r := httptest.NewRequest(http.MethodPost, "/v1/set-volume", strings.NewReader(body))
		r.SetBasicAuth(DefaultUsername, DefaultPassword)
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
}

func TestLocalPlayerFailuresAreNotSuccessOrIdle(t *testing.T) {
	b, d, dev, dir := controlBridge(t)
	ctx := context.Background()
	b.dev.Mphelper = script(t, dir, "failing-player", "exit 1")
	if _, err := d.PlayerGetStatus(ctx, dev); err == nil {
		t.Fatal("player error reported as idle")
	}
	if err := d.PlayerPause(ctx, dev); err == nil {
		t.Fatal("pause command failure suppressed")
	}
	if err := d.PlayerPlay(ctx, dev); err == nil {
		t.Fatal("play command failure suppressed")
	}
	if err := d.PlayerStop(ctx, dev); err == nil {
		t.Fatal("stop warning suppressed")
	}
	for _, out := range []string{`{"code":7}`, `not JSON`, `null`} {
		b.dev.Ubus = script(t, dir, "bad-ubus", "printf '%s' '"+out+"'")
		if err := d.PlayerSetVolume(ctx, dev, 50); err == nil {
			t.Errorf("accepted ubus failure: %s", out)
		}
		if err := d.PlayByURL(ctx, dev, "http://nas/song"); err == nil {
			t.Errorf("accepted playback failure: %s", out)
		}
	}
	b.dev.Mphelper = script(t, dir, "unknown-player", "echo status=123")
	if _, err := d.PlayerGetStatus(ctx, dev); err == nil {
		t.Fatal("ambiguous status accepted")
	}
}

func TestCanceledSpeechDoesNotStartPlayback(t *testing.T) {
	b, _, _, _ := controlBridge(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.dev.Speak(ctx, "must not start", false); err == nil {
		t.Fatal("started TTS for canceled request")
	}
	if _, _, speaking := b.dev.Speaking(); speaking {
		t.Fatal("canceled request left active playback")
	}
}

func TestSpeakRunsShebanglessScriptWithLiteralText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tts script without shebang.sh")
	if err := os.WriteFile(path, []byte("printf '%s' \"$1\" > \"$0.received\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "must-not-execute")
	text := "你好 'quoted' \"text\"; $(touch " + marker + ")\nsecond line"
	d := &Device{TTSPlay: path}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	interrupted, err := d.Speak(ctx, text, true)
	if err != nil || interrupted {
		t.Fatalf("Speak: interrupted=%v err=%v", interrupted, err)
	}
	received, err := os.ReadFile(path + ".received")
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != text {
		t.Fatalf("received %q, want %q", received, text)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("text was evaluated as shell source: %v", err)
	}
}

func TestRealBridgeStreamOnlyFutureFinals(t *testing.T) {
	b, d, dev, _ := controlBridge(t)
	b.hub.Publish(Event{Type: "asr", Text: "history", Final: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	asks := make(chan speaker.Ask, 2)
	done := make(chan error, 1)
	go func() { done <- d.StreamAsks(ctx, dev, func(a speaker.Ask) { asks <- a }) }()
	waitFor(t, "stream subscribed", func() bool { return b.hub.Subscribers() == 1 })
	b.hub.Publish(Event{Type: "asr", Text: "partial", Final: false})
	b.hub.Publish(Event{Type: "asr", Text: "future", Device: "remote-name", Final: true, TS: 1})
	select {
	case a := <-asks:
		if a.Query != "future" || a.DeviceID != dev.DeviceID {
			t.Fatalf("received %+v", a)
		}
	case <-ctx.Done():
		t.Fatal("future final not delivered")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancellation not returned")
	}
	select {
	case a := <-asks:
		t.Fatalf("unexpected extra ask: %+v", a)
	default:
	}
}

func TestStatusEndpointPreservesPlayerError(t *testing.T) {
	b, _, _, dir := controlBridge(t)
	b.dev.Mphelper = script(t, dir, "failed-status", "exit 1")
	r := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	r.SetBasicAuth(DefaultUsername, DefaultPassword)
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	var out struct {
		Status StatusReport `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Status.Player != "" || out.Status.PlayerError == "" {
		t.Fatalf("status: %+v", out.Status)
	}
}
