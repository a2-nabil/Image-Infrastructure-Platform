package middleware

import (
	"net/http"
	"strings"
)

// CORSMiddleware adds CORS headers for configured origins and handles OPTIONS preflight.
// If allowedOrigins is empty, defaults to http://localhost:3000 (local development).
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	origins := normalizeOrigins(allowedOrigins)
	if len(origins) == 0 {
		origins = []string{"http://localhost:3000"}
	}
	allowAll := false
	set := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		if o == "*" {
			allowAll = true
			break
		}
		set[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqOrigin := r.Header.Get("Origin")
			reflected := false
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if reqOrigin != "" {
				if _, ok := set[reqOrigin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", reqOrigin)
					w.Header().Set("Vary", "Origin")
					reflected = true
				}
			} else if len(origins) == 1 {
				// Non-browser clients: still advertise the single configured origin.
				w.Header().Set("Access-Control-Allow-Origin", origins[0])
				w.Header().Set("Vary", "Origin")
				reflected = true
			}

			// Required when the browser uses fetch(..., { credentials: "include" }).
			// Must never be combined with Allow-Origin: *.
			if reflected && !allowAll {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, Authorization, X-Guest-Session-ID")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-Guest-Session-ID, X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func normalizeOrigins(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		o := strings.TrimRight(strings.TrimSpace(raw), "/")
		if o == "" {
			continue
		}
		if _, ok := seen[o]; ok {
			continue
		}
		seen[o] = struct{}{}
		out = append(out, o)
	}
	return out
}
