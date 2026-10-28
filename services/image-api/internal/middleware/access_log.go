package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"

	"image-infrastructure-platform/services/image-api/internal/metrics"
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

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"route", route,
				"status", rec.status,
				"duration_ms", duration.Milliseconds(),
				"bytes", rec.bytes,
				"request_id", GetRequestID(r.Context()),
				"remote_ip", clientIP(r),
				"user_agent", r.UserAgent(),
			}
			if uid := GetUserID(r.Context()); uid != nil {
				attrs = append(attrs, "user_id", uid.String())
			}
			if guest := GetGuestSessionID(r.Context()); guest != "" {
				attrs = append(attrs, "guest_session_id", guest)
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
