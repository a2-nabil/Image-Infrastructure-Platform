package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/middleware"
)

// New constructs a structured logger for the given environment and level,
// sets it as the process default, and returns it.
func New(appEnv string, level string) *slog.Logger {
	return newLogger(appEnv, level, os.Stdout)
}

func newLogger(appEnv string, level string, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var base slog.Handler
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production", "staging":
		base = slog.NewJSONHandler(w, opts)
	default:
		base = slog.NewTextHandler(w, opts)
	}

	logger := slog.New(&contextHandler{inner: base})
	slog.SetDefault(logger)
	return logger
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// contextHandler wraps an slog.Handler and injects request_id from context.
type contextHandler struct {
	inner slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := middleware.GetRequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.inner.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{inner: h.inner.WithGroup(name)}
}
