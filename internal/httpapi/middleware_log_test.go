package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogErrors(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := accessLog(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			writeError(w, 500, "kaput")
			return
		}
		writeError(w, 400, "bad")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/boom", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	out := buf.String()
	if !strings.Contains(out, `"level":"ERROR","msg":"http error"`) || !strings.Contains(out, "kaput") {
		t.Fatalf("missing 5xx error log: %s", out)
	}
	if !strings.Contains(out, `"level":"DEBUG","msg":"http error"`) {
		t.Fatalf("missing 4xx debug log: %s", out)
	}
}
