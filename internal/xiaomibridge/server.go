package xiaomibridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const sseHeartbeat = 15 * time.Second

// Handler serves the API. Everything except GET /healthz requires HTTP Basic
// auth with the configured credentials.
//
//	GET  /healthz
//	GET  /v1/config                 settings without secrets
//	PUT  /v1/config                 partial update, persisted to the config file
//	POST /v1/tts         {"text","wait"}  speak, interrupting the previous TTS
//	POST /v1/stop                   interrupt playback
//	POST /v1/play-url    {"url"}
//	POST /v1/abort-xiaoai
//	GET  /v1/asr/latest             latest recognized user input
//	GET  /v1/asr/stream             SSE stream of user input (?partial=1, ?last=1)
//	GET  /v1/status                 internal state
//	GET  /v1/mcp.json               MCP client config + tool list + curl examples
//	POST /mcp                       MCP (streamable HTTP)
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"ok": true})
	})
	api := http.NewServeMux()
	api.HandleFunc("GET /v1/config", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{"ok": true, "config": b.store.View()})
	})
	api.HandleFunc("PUT /v1/config", b.putConfig)
	api.HandleFunc("POST /v1/tts", b.postTTS)
	api.HandleFunc("POST /v1/stop", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, b.stop(r.Context()))
	})
	api.HandleFunc("POST /v1/play-url", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if !decode(w, r, &in) {
			return
		}
		if !httpURL(in.URL) {
			fail(w, http.StatusBadRequest, "url must be http(s)")
			return
		}
		if err := b.dev.PlayURL(r.Context(), in.URL); err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true})
	})
	api.HandleFunc("POST /v1/abort-xiaoai", func(w http.ResponseWriter, r *http.Request) {
		if err := b.dev.AbortXiaoAI(context.WithoutCancel(r.Context())); err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		reply(w, http.StatusOK, map[string]any{"ok": true})
	})
	api.HandleFunc("GET /v1/asr/latest", func(w http.ResponseWriter, _ *http.Request) {
		latest, finals := b.hub.Latest()
		reply(w, http.StatusOK, map[string]any{"ok": true, "input": latest, "finals": finals})
	})
	api.HandleFunc("GET /v1/asr/stream", b.stream)
	api.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, map[string]any{"ok": true, "status": b.Status(r.Context())})
	})
	api.HandleFunc("GET /v1/mcp.json", b.mcpDescriptor)
	api.Handle("/mcp", b.mcpHandler())
	mux.Handle("/", b.basicAuth(api))
	return mux
}

func (b *Bridge) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := b.store.Get().Auth
		u, p, ok := r.BasicAuth()
		uOK := subtle.ConstantTimeCompare([]byte(u), []byte(a.Username))
		pOK := subtle.ConstantTimeCompare([]byte(p), []byte(a.Password))
		if !ok || uOK&pOK != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="xiaomi-bridge", charset="UTF-8"`)
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		next.ServeHTTP(w, r)
	})
}

func (b *Bridge) putConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := DecodePatch(body)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid config patch: "+err.Error())
		return
	}
	if err := b.store.Update(p); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	b.log.Info("config updated", "config", b.store.View())
	reply(w, http.StatusOK, map[string]any{"ok": true, "config": b.store.View()})
}

type ttsIn struct {
	Text string `json:"text" jsonschema:"text to speak"`
	Wait bool   `json:"wait,omitempty" jsonschema:"return only after playback ended (default: return once it started)"`
}

type ttsOut struct {
	Interrupted bool `json:"interrupted" jsonschema:"with wait: playback was cut short by stop or a newer speak"`
}

func (b *Bridge) postTTS(w http.ResponseWriter, r *http.Request) {
	var in ttsIn
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		fail(w, http.StatusBadRequest, errNoText.Error())
		return
	}
	out, err := b.speak(r.Context(), in)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true, "interrupted": out.Interrupted})
}

var errNoText = errors.New("text required")

func (b *Bridge) speak(ctx context.Context, in ttsIn) (ttsOut, error) {
	if strings.TrimSpace(in.Text) == "" {
		return ttsOut{}, errNoText
	}
	interrupted, err := b.dev.Speak(ctx, in.Text, in.Wait)
	return ttsOut{Interrupted: interrupted}, err
}

type stopOut struct {
	OK          bool   `json:"ok"`
	Interrupted bool   `json:"interrupted" jsonschema:"the bridge's TTS was playing and got killed"`
	Warning     string `json:"warning,omitempty" jsonschema:"pausing/stopping the system player failed"`
}

// stop is detached from the caller: a client giving up must not leave
// playback half-stopped.
func (b *Bridge) stop(ctx context.Context) stopOut {
	interrupted, err := b.dev.Stop(context.WithoutCancel(ctx))
	out := stopOut{OK: true, Interrupted: interrupted}
	if err != nil {
		out.Warning = err.Error()
		b.log.Warn("stop partially failed", "err", err)
	}
	return out
}

// stream serves user input as Server-Sent Events ("event: asr").
func (b *Bridge) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	q := r.URL.Query()
	ch, latest, cancel := b.hub.Subscribe(q.Get("partial") == "1")
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(ev Event) bool {
		data, _ := json.Marshal(ev)
		_, err := fmt.Fprintf(w, "id: %d\nevent: asr\ndata: %s\n\n", ev.Seq, data)
		fl.Flush()
		return err == nil
	}
	_, _ = io.WriteString(w, ": connected\n\n")
	if q.Get("last") == "1" && latest != nil && !send(*latest) {
		return
	}
	fl.Flush()
	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !send(ev) {
				return
			}
		case <-tick.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]any{"ok": false, "error": msg})
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
