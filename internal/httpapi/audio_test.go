package httpapi

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andy/vox-mi-agent/internal/mediaurl"
)

func TestServeAudio(t *testing.T) {
	e := syncedEnv(t, "周杰伦 - 晴天.mp3", "Alan Walker - Alone.flac")
	var mp3, flac string
	var mp3Path string
	for _, tr := range e.app.Library.Tracks() {
		if strings.HasSuffix(tr.Path, ".mp3") {
			mp3, mp3Path = tr.ID, tr.Path
		} else {
			flac = tr.ID
		}
	}
	if err := os.WriteFile(mp3Path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	sec := e.app.Secret()
	signed := func(id, ext string, exp time.Time) string {
		u := mediaurl.Sign(sec, "", id, ext, exp)
		return u
	}
	good := signed(mp3, "mp3", time.Now().Add(time.Hour))

	resp, body := e.do(t, "GET", good, "")
	if resp.StatusCode != 200 || string(body) != "0123456789" || resp.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("%d %q %v", resp.StatusCode, body, resp.Header)
	}
	resp, body = e.do(t, "GET", good, "", "Range", "bytes=2-4")
	if resp.StatusCode != 206 || string(body) != "234" {
		t.Fatalf("range %d %q", resp.StatusCode, body)
	}
	if resp, _ = e.do(t, "HEAD", good, ""); resp.StatusCode != 200 {
		t.Fatalf("head %d", resp.StatusCode)
	}
	cases := map[string]struct {
		url  string
		code int
	}{
		"tampered sig": {strings.Replace(good, "sig=", "sig=A", 1), 403},
		"no sig":       {"/audio/" + mp3 + ".mp3", 403},
		"expired":      {signed(mp3, "mp3", time.Now().Add(-time.Minute)), 403},
		"wrong ext":    {signed(mp3, "flac", time.Now().Add(time.Hour)), 404},
		"unknown id":   {signed("nope", "mp3", time.Now().Add(time.Hour)), 404},
		"flac ok id":   {signed(flac, "flac", time.Now().Add(time.Hour)), 200},
	}
	for name, c := range cases {
		if resp, _ := e.do(t, "GET", c.url, ""); resp.StatusCode != c.code {
			t.Errorf("%s: %d want %d", name, resp.StatusCode, c.code)
		}
	}
	_ = http.StatusOK
}
