package httpapi

import (
	"bufio"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/andy/vox-mi-agent/internal/logx"
)

// stripBase removes basePath from the request path when present; other
// requests pass through unchanged.
func stripBase(basePath string, next http.Handler) http.Handler {
	bp := strings.TrimRight(basePath, "/")
	if bp != "" && !strings.HasPrefix(bp, "/") {
		bp = "/" + bp
	}
	if bp == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == bp || strings.HasPrefix(p, bp+"/") {
			r2 := new(http.Request)
			*r2 = *r
			u := *r.URL
			u.Path = strings.TrimPrefix(p, bp)
			if u.Path == "" {
				u.Path = "/"
			}
			if u.RawPath != "" {
				u.RawPath = strings.TrimPrefix(u.RawPath, bp)
				if u.RawPath == "" {
					u.RawPath = "/"
				}
			}
			r2.URL = &u
			r = r2
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter records status and size; it forwards Flush and exposes the
// underlying writer for http.ResponseController.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status, w.wrote = http.StatusOK, true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = logx.NewID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(logx.WithRequestID(r.Context(), id)))
	})
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					log.ErrorContext(r.Context(), "panic", "panic", p, "stack", string(debug.Stack()))
					if sw, ok := w.(*statusWriter); !ok || !sw.wrote {
						writeError(w, http.StatusInternalServerError, "internal error")
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				log.InfoContext(r.Context(), "http",
					"method", r.Method, "path", r.URL.Path, "status", sw.status,
					"bytes", sw.bytes, "duration_ms", time.Since(start).Milliseconds(),
					"remote", r.RemoteAddr)
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/sse" || strings.HasPrefix(p, "/sse/")
}

// adminOnly enforces X-Trim-Isadmin == "true" on /api and /sse.
func adminOnly(require bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if require && isAPIPath(r.URL.Path) && r.Header.Get("X-Trim-Isadmin") != "true" {
				writeError(w, http.StatusForbidden, "admin required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
