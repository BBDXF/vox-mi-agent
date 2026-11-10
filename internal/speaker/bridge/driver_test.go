package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BBDXF/vox-mi-agent/internal/speaker"
)

var testDevice = speaker.Device{DeviceID: "local-living", Name: "Living room", Enabled: true}

func testDriver(t *testing.T, h http.HandlerFunc) *Driver {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "secret" {
			t.Error("missing explicit Basic auth")
			w.WriteHeader(401)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return NewDriver([]Endpoint{{Device: testDevice, URL: s.URL, Username: "user", Password: "secret"}})
}

func TestControlsRejectUnknownDeviceAndInvalidVolume(t *testing.T) {
	d := NewDriver([]Endpoint{{Device: testDevice, URL: "http://127.0.0.1:1", Username: "user", Password: "secret"}})
	ctx := context.Background()
	if err := d.PlayerSetVolume(ctx, testDevice, 101); err == nil {
		t.Fatal("accepted invalid volume")
	}
	if err := d.PlayerPause(ctx, speaker.Device{DeviceID: "remote-name"}); !errors.Is(err, speaker.ErrNoDevice) {
		t.Fatalf("unknown device: %v", err)
	}
}

func TestControlRejectsMalformedAndFailureResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, content string
		code                int
	}{
		{"auth", `{"ok":false}`, "application/json", 401},
		{"upstream", `{"ok":false}`, "application/json", 502},
		{"missing-ok", `{}`, "application/json", 200},
		{"false", `{"ok":false}`, "application/json", 200},
		{"warning", `{"ok":true,"warning":"secret"}`, "application/json", 200},
		{"error", `{"ok":true,"error":"secret"}`, "application/json", 200},
		{"invalid", `{`, "application/json", 200},
		{"trailing", `{"ok":true}{}`, "application/json", 200},
		{"html", `{"ok":true}`, "text/html", 200},
		{"large", strings.Repeat(" ", maxResponse+1), "application/json", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.content)
				w.WriteHeader(tc.code)
				io.WriteString(w, tc.body)
			})
			err := d.PlayerStop(context.Background(), testDevice)
			if err == nil {
				t.Fatal("accepted failed response")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("remote error text leaked")
			}
		})
	}
}

func TestEndpointValidationAndRedirects(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if err := d.PlayerPause(context.Background(), testDevice); err == nil || hit {
		t.Fatalf("redirect followed: %v, hit %v", err, hit)
	}
	for _, ep := range []Endpoint{
		{Device: testDevice, URL: target.URL, Username: "user"},
		{Device: testDevice, URL: target.URL, Password: "secret"},
		{Device: testDevice, URL: target.URL + "?token=secret", Username: "user", Password: "secret"},
		{Device: testDevice, URL: "http://user:secret@localhost", Username: "user", Password: "secret"},
		{Device: testDevice, URL: target.URL + "#fragment", Username: "user", Password: "secret"},
	} {
		if err := NewDriver([]Endpoint{ep}).PlayerPause(context.Background(), testDevice); err == nil {
			t.Fatalf("accepted invalid endpoint: %s", ep.URL)
		}
	}
	if hit {
		t.Fatal("invalid endpoint made request")
	}
}

func TestStatusNeverConfusesTTSOrPlayerErrorsWithIdle(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		fail   bool
	}{
		{`{"tts":{"speaking":true},"player":"idle"}`, speaker.StatusPlaying, false},
		{`{"tts":{"speaking":true},"player_error":"failed"}`, speaker.StatusPlaying, false},
		{`{"tts":{"speaking":false},"player":"playing"}`, speaker.StatusPlaying, false},
		{`{"tts":{"speaking":false},"player":"paused"}`, speaker.StatusPaused, false},
		{`{"tts":{"speaking":false},"player":"idle"}`, speaker.StatusIdle, false},
		{`{"tts":{"speaking":false},"player_error":"failed"}`, 0, true},
		{`{"tts":{"speaking":false},"player":"unknown"}`, 0, true},
		{`{}`, 0, true},
		{`null`, 0, true},
	} {
		d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"status":%s}`, tc.body)
		})
		st, err := d.PlayerGetStatus(context.Background(), testDevice)
		if (err != nil) != tc.fail || (!tc.fail && st.Status != tc.status) {
			t.Fatalf("%s: %+v, %v", tc.body, st, err)
		}
	}
}

func TestRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	d := testDriver(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.PlayerPause(ctx, testDevice) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("control ignored cancellation")
	}
}

func TestStreamFinalFilteringOrderAndRestart(t *testing.T) {
	connections := 0
	d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
		connections++
		if r.URL.Path != "/v1/asr/stream" || r.URL.RawQuery != "" || r.Header.Get("Last-Event-ID") != "" {
			t.Errorf("replaying stream request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		io.WriteString(w, ": heartbeat\r\n\r\nevent: other\ndata: not JSON\n\n")
		io.WriteString(w, "event: asr\ndata: {\ndata: \"type\":\"asr\",\"text\":\"first\",\"device\":\"remote\",\"final\":true,\"seq\":99,\"ts\":9999999999999}\n\n")
		io.WriteString(w, "event: asr\ndata: {\"type\":\"asr\",\"text\":\"partial\",\"final\":false}\n\n")
		io.WriteString(w, "event: asr\ndata: {\"type\":\"asr\",\"text\":\" \",\"final\":true}\n\n")
		io.WriteString(w, "event: asr\ndata: {\"type\":\"other\",\"text\":\"not asr\",\"final\":true}\n\n")
		io.WriteString(w, "event: asr\ndata: {\"type\":\"asr\",\"text\":\"second\",\"final\":true,\"seq\":1,\"ts\":1}\n\n")
		io.WriteString(w, "event: asr\ndata: {\"type\":\"asr\",\"text\":\"incomplete\",\"final\":true}")
	})
	var asks []speaker.Ask
	before := time.Now()
	for range 2 {
		err := d.StreamAsks(context.Background(), testDevice, func(a speaker.Ask) { asks = append(asks, a) })
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	}
	if connections != 2 || len(asks) != 4 {
		t.Fatalf("connections=%d asks=%+v", connections, asks)
	}
	for i, a := range asks {
		want := "first"
		if i%2 == 1 {
			want = "second"
		}
		if a.DeviceID != testDevice.DeviceID || a.Query != want || a.Time.Before(before) || a.Time.After(time.Now()) {
			t.Fatalf("ask[%d] = %+v", i, a)
		}
		if i > 0 && !a.Time.After(asks[i-1].Time) {
			t.Fatal("receipt order lost")
		}
	}
}

func TestStreamErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		content, body string
		code          int
	}{
		{"text/event-stream", "event: asr\ndata: bad JSON\n\n", 200},
		{"text/event-stream", "data: " + strings.Repeat("x", maxEvent+1) + "\n\n", 200},
		{"text/event-stream", strings.Repeat("x: "+strings.Repeat("x", 1000)+"\n", 70) + "\n", 200},
		{"application/json", `{}`, 200},
		{"text/event-stream", "", 401},
	} {
		d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.content)
			w.WriteHeader(tc.code)
			io.WriteString(w, tc.body)
		})
		if err := d.StreamAsks(context.Background(), testDevice, func(speaker.Ask) { t.Error("unexpected ASR") }); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("expected stream error, got %v", err)
		}
	}
	started := make(chan struct{})
	d := testDriver(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": connected\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.StreamAsks(ctx, testDevice, func(speaker.Ask) {}) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream ignored cancellation")
	}
	if d.client.Timeout != 0 {
		t.Fatal("stream client has an overall timeout")
	}
}
