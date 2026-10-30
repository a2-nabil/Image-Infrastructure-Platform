package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"image-infrastructure-platform/services/api/internal/metrics"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// AccessLogMiddleware emits structured request access logs and records HTTP metrics.
func AccessLogMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			duration := time.Since(start)

			route := r.Pattern
			if route == "" {
				route = r.URL.Path
			}
			metrics.Default.ObserveHTTP(r.Method, route, rec.status, duration)

			// Skip noisy probes unless they fail.
			if (r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics") &&
				rec.status < 400 {
				return
			}

			// Sanitize user-controlled values before logging (CWE-117 / go/log-injection).
			attrs := []any{
				"method", sanitizeForLog(r.Method, 16),
				"path", sanitizeForLog(r.URL.Path, 512),
				"route", sanitizeForLog(route, 256),
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", rec.bytes,
				"request_id", sanitizeForLog(GetRequestID(r.Context()), 128),
				"remote_ip", sanitizeForLog(clientIP(r), 128),
				"user_agent", sanitizeForLog(r.UserAgent(), 256),
			}
			if uid := GetUserID(r.Context()); uid != nil {
				attrs = append(attrs, "user_id", sanitizeForLog(uid.String(), 64))
			}
			if guest := GetGuestSessionID(r.Context()); guest != "" {
				attrs = append(attrs, "guest_session_id", sanitizeForLog(guest, 128))
			}

			switch {
			case rec.status >= 500:
				logger.ErrorContext(r.Context(), "http_access", attrs...)
			case rec.status >= 400:
				logger.WarnContext(r.Context(), "http_access", attrs...)
			default:
				logger.InfoContext(r.Context(), "http_access", attrs...)
			}
		})
	}
}

// sanitizeForLog strips control characters (especially CR/LF) so clients cannot
// forge additional log lines, and truncates overly long values.
func sanitizeForLog(value string, maxLen int) string {
	if value == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 256
	}

	var b strings.Builder
	b.Grow(min(len(value), maxLen))
	for _, r := range value {
		switch {
		case r == '\n', r == '\r':
			b.WriteByte(' ')
		case unicode.IsControl(r):
			// Drop other control characters (tabs become space for readability).
			if r == '\t' {
				b.WriteByte(' ')
			}
		case !utf8.ValidRune(r):
			b.WriteByte('?')
		default:
			b.WriteRune(r)
		}
		if b.Len() >= maxLen {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
