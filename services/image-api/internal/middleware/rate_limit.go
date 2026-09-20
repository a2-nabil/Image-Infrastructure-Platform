package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Fixed-window counter: INCR, EXPIRE on first hit, return {count, ttl}.
var rateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("EXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("TTL", KEYS[1])
return {current, ttl}
`)

// RateLimitMiddleware enforces a fixed-window per-IP limit using Redis.
func RateLimitMiddleware(rdb *redis.Client, limit int, window time.Duration, bucketName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			ip := clientIP(r)
			key := fmt.Sprintf("ratelimit:%s:%s", bucketName, ip)
			windowSeconds := int64(window.Seconds())
			if windowSeconds < 1 {
				windowSeconds = 1
			}

			result, err := rateLimitScript.Run(r.Context(), rdb, []string{key}, windowSeconds).Slice()
			if err != nil {
				// Fail open if Redis is unavailable so the API remains usable.
				next.ServeHTTP(w, r)
				return
			}

			current := toInt64(result[0])
			ttlSeconds := toInt64(result[1])
			if ttlSeconds < 0 {
				ttlSeconds = windowSeconds
			}

			remaining := int64(limit) - current
			if remaining < 0 {
				remaining = 0
			}

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", ttlSeconds))

			if current > int64(limit) {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", ttlSeconds))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code":    "RATE_LIMIT_EXCEEDED",
						"message": "Too many requests. Please try again later.",
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		var parsed int64
		_, _ = fmt.Sscanf(n, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

// PingRedis verifies Redis connectivity (used at startup).
func PingRedis(ctx context.Context, rdb *redis.Client) error {
	if rdb == nil {
		return fmt.Errorf("redis client is nil")
	}
	return rdb.Ping(ctx).Err()
}
