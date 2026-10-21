// Package logx provides the JSON slog logger and request/turn context helpers.
package logx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"strings"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	turnIDKey
)

// WithRequestID returns ctx carrying the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// WithTurnID returns ctx carrying the conversation turn id.
func WithTurnID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnIDKey, id)
}

// RequestID returns the request id in ctx, or "".
func RequestID(ctx context.Context) string { s, _ := ctx.Value(requestIDKey).(string); return s }

// TurnID returns the turn id in ctx, or "".
func TurnID(ctx context.Context) string { s, _ := ctx.Value(turnIDKey).(string); return s }

// NewID returns a random 12-hex-char identifier.
func NewID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ParseLevel maps debug/info/warn/error (case-insensitive) to slog.Level; unknown -> Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}

// New returns a JSON logger writing to w whose records gain request_id /
// turn_id attributes when the *Context logging methods are used with a ctx
// carrying them.
func New(level string, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: ParseLevel(level)})
	return slog.New(NewContextHandler(h))
}

// NewWithLevelVar is like New but the level can be changed at runtime via lv.
func NewWithLevelVar(lv *slog.LevelVar, w io.Writer) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lv})
	return slog.New(NewContextHandler(h))
}

// NewContextHandler wraps h to add request_id/turn_id from the record ctx.
func NewContextHandler(h slog.Handler) slog.Handler { return &ctxHandler{h} }

type ctxHandler struct{ slog.Handler }

func (h *ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if id := TurnID(ctx); id != "" {
		r.AddAttrs(slog.String("turn_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *ctxHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &ctxHandler{h.Handler.WithAttrs(a)}
}
func (h *ctxHandler) WithGroup(n string) slog.Handler { return &ctxHandler{h.Handler.WithGroup(n)} }
