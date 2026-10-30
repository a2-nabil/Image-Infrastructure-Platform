package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/api/internal/auth"
)

type authContextKey string

const (
	userIDKey         authContextKey = "user_id"
	guestSessionIDKey authContextKey = "guest_session_id"
)

const (
	guestSessionHeader = "X-Guest-Session-ID"
	GuestSessionCookie = "guest_session_id"
)

// OptionalAuthMiddleware extracts optional JWT user identity and guest session id.
func OptionalAuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if authHeader := r.Header.Get("Authorization"); authHeader != "" && jwtSecret != "" {
				tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
				if tokenString != "" && tokenString != authHeader {
					if userID, ok := auth.ParseUserID(tokenString, jwtSecret); ok {
						ctx = context.WithValue(ctx, userIDKey, userID)
					}
				}
			}

			guestID := strings.TrimSpace(r.Header.Get(guestSessionHeader))
			if guestID == "" {
				if c, err := r.Cookie(GuestSessionCookie); err == nil {
					guestID = strings.TrimSpace(c.Value)
				}
			}
			if guestID != "" {
				ctx = context.WithValue(ctx, guestSessionIDKey, guestID)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects requests without an authenticated user_id in context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetUserID(r.Context()) == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error": map[string]string{
					"code":    "UNAUTHORIZED",
					"message": "authentication required",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GalleryAccessChecker decides whether an authenticated user may use gallery APIs.
type GalleryAccessChecker interface {
	UserMayAccessGallery(ctx context.Context, userID uuid.UUID) (allowed bool, err error)
}

// RequireGalleryAccess requires auth and gallery allowlist (or super admin).
func RequireGalleryAccess(checker GalleryAccessChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			if userID == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code":    "UNAUTHORIZED",
						"message": "authentication required",
					},
				})
				return
			}
			if checker == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code":    "GALLERY_FORBIDDEN",
						"message": "gallery access is not configured",
					},
				})
				return
			}
			ok, err := checker.UserMayAccessGallery(r.Context(), *userID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code":    "GALLERY_ACCESS_CHECK_FAILED",
						"message": "failed to verify gallery access",
					},
				})
				return
			}
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"error": map[string]string{
						"code":    "GALLERY_FORBIDDEN",
						"message": "gallery access denied",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetUserID returns the authenticated user UUID from context, if present.
func GetUserID(ctx context.Context) *uuid.UUID {
	if ctx == nil {
		return nil
	}
	if id, ok := ctx.Value(userIDKey).(uuid.UUID); ok {
		copied := id
		return &copied
	}
	if id, ok := ctx.Value(userIDKey).(*uuid.UUID); ok {
		return id
	}
	return nil
}

// GetGuestSessionID returns the guest session id from context, or empty string.
func GetGuestSessionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(guestSessionIDKey).(string); ok {
		return id
	}
	return ""
}
