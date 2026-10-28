package xiaomibridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// ControlHandler serves the local control API. Every route except /healthz
// requires "Authorization: Bearer <token>"; token must be non-empty.
//
//	POST /v1/tts           {"text":"...","mode":"async"|"blocking"}
//	POST /v1/play-url      {"url":"http://..."}
//	POST /v1/stop
//	POST /v1/abort-xiaoai
//	GET  /v1/status
func ControlHandler(dev *Device, token string, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"ok": true})
	})
	auth := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				reply(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			h(w, r)
		}
	}
	fail := func(w http.ResponseWriter, op string, err error) {
		log.Warn("control failed", "op", op, "err", err)
		reply(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
	}
	mux.HandleFunc("POST /v1/tts", auth(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Text) == "" {
			reply(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text required"})
			return
		}
		switch in.Mode {
		case "", "async":
			if err := dev.Speak(r.Context(), in.Text); err != nil {
				fail(w, "tts", err)
				return
			}
			reply(w, http.StatusOK, map[string]any{"ok": true})
		case "blocking":
			interrupted, err := dev.SpeakBlocking(r.Context(), in.Text)
			if err != nil {
				fail(w, "tts", err)
				return
			}
			reply(w, http.StatusOK, map[string]any{"ok": true, "interrupted": interrupted})
		default:
			reply(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "mode must be async or blocking"})
		}
	}))
	mux.HandleFunc("POST /v1/play-url", auth(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || !httpURL(in.URL) {
			reply(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "http(s) url required"})
			return
		}
		if err := dev.PlayURL(r.Context(), in.URL); err != nil {
			fail(w, "play-url", err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mux.HandleFunc("POST /v1/stop", auth(func(w http.ResponseWriter, r *http.Request) {
		// Detached: a client giving up must not leave playback half-stopped.
		interrupted, err := dev.Stop(context.WithoutCancel(r.Context()))
		if err != nil {
			log.Warn("control stop partially failed", "err", err)
			reply(w, http.StatusOK, map[string]any{"ok": true, "interrupted": interrupted, "warning": err.Error()})
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true, "interrupted": interrupted})
	}))
	mux.HandleFunc("POST /v1/abort-xiaoai", auth(func(w http.ResponseWriter, r *http.Request) {
		if err := dev.AbortXiaoAI(context.WithoutCancel(r.Context())); err != nil {
			fail(w, "abort-xiaoai", err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true})
	}))
	mux.HandleFunc("GET /v1/status", auth(func(w http.ResponseWriter, r *http.Request) {
		st, err := dev.Status(r.Context())
		if err != nil {
			fail(w, "status", err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true, "status": st})
	}))
	return mux
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
