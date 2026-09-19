package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"image-infrastructure-platform/services/image-api/internal/middleware"
)

func TestNewUsesJSONHandlerForStagingAndProduction(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		t.Run(env, func(t *testing.T) {
			var buf bytes.Buffer
			log := newLogger(env, "info", &buf)
			log.Info("json-check", "ok", true)

			line := strings.TrimSpace(buf.String())
			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				t.Fatalf("expected JSON log for %s, got %q: %v", env, line, err)
			}
			if payload["msg"] != "json-check" {
				t.Errorf("msg = %v, want %q", payload["msg"], "json-check")
			}
		})
	}
}

func TestNewUsesTextHandlerForDevelopment(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger("development", "info", &buf)
	log.Info("text-check")

	line := strings.TrimSpace(buf.String())
	if json.Valid([]byte(line)) {
		t.Fatalf("expected text log for development, got JSON: %q", line)
	}
	if !strings.Contains(line, "text-check") {
		t.Errorf("log line missing message: %q", line)
	}
}

func TestContextRequestIDIncludedInLogOutput(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger("production", "info", &buf)

	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "req-123")
	log.InfoContext(ctx, "with-request-id")

	line := strings.TrimSpace(buf.String())
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("expected JSON log, got %q: %v", line, err)
	}
	if payload["request_id"] != "req-123" {
		t.Errorf("request_id = %v, want %q", payload["request_id"], "req-123")
	}
	if payload["msg"] != "with-request-id" {
		t.Errorf("msg = %v, want %q", payload["msg"], "with-request-id")
	}
}

func TestParseLevelDefaultsToInfo(t *testing.T) {
	if got := parseLevel(""); got != slog.LevelInfo {
		t.Errorf("parseLevel(\"\") = %v, want %v", got, slog.LevelInfo)
	}
	if got := parseLevel("nope"); got != slog.LevelInfo {
		t.Errorf("parseLevel(\"nope\") = %v, want %v", got, slog.LevelInfo)
	}
	if got := parseLevel("debug"); got != slog.LevelDebug {
		t.Errorf("parseLevel(\"debug\") = %v, want %v", got, slog.LevelDebug)
	}
}
