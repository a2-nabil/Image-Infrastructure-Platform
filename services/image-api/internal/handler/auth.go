package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/api/idtoken"

	"image-infrastructure-platform/services/image-api/internal/auth"
	"image-infrastructure-platform/services/image-api/internal/service"
)

const defaultTokenTTL = 24 * time.Hour

// AuthHandler serves authentication endpoints.
type AuthHandler struct {
	users          *service.UserService
	jwtSecret      string
	googleClientID string
	appEnv         string
	tokenTTL       time.Duration
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(users *service.UserService, jwtSecret, googleClientID, appEnv string) *AuthHandler {
	return &AuthHandler{
		users:          users,
		jwtSecret:      jwtSecret,
		googleClientID: googleClientID,
		appEnv:         appEnv,
		tokenTTL:       defaultTokenTTL,
	}
}

type googleAuthRequest struct {
	IDToken string `json:"id_token"`
}

type devTokenRequest struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

// GoogleAuthHandler verifies a Google ID token, upserts the user, and returns a JWT.
func (h *AuthHandler) GoogleAuthHandler(w http.ResponseWriter, r *http.Request) {
	if h.jwtSecret == "" {
		writeError(w, http.StatusInternalServerError, "AUTH_MISCONFIGURED", "JWT secret is not configured")
		return
	}
	if h.googleClientID == "" {
		writeError(w, http.StatusInternalServerError, "AUTH_MISCONFIGURED", "Google client ID is not configured")
		return
	}

	var req googleAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if req.IDToken == "" {
		writeError(w, http.StatusBadRequest, "MISSING_ID_TOKEN", "id_token is required")
		return
	}

	payload, err := idtoken.Validate(r.Context(), req.IDToken, h.googleClientID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_GOOGLE_TOKEN", "google id_token verification failed")
		return
	}

	googleID := payload.Subject
	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)
	if googleID == "" || email == "" {
		writeError(w, http.StatusUnauthorized, "INVALID_GOOGLE_TOKEN", "google token missing required claims")
		return
	}

	user, err := h.users.UpsertGoogleUser(r.Context(), googleID, email, name, picture)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "USER_UPSERT_FAILED", err.Error())
		return
	}

	userUUID, err := uuid.Parse(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INVALID_USER_ID", "stored user id is invalid")
		return
	}

	token, err := auth.GenerateToken(userUUID, user.Email, h.jwtSecret, h.tokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   int(h.tokenTTL.Seconds()),
			"user": map[string]any{
				"id":         user.ID,
				"email":      user.Email,
				"name":       user.Name,
				"avatar_url": user.AvatarURL,
			},
		},
	})
}

// DevTokenHandler issues a JWT for local/testing use outside production.
func (h *AuthHandler) DevTokenHandler(w http.ResponseWriter, r *http.Request) {
	if h.appEnv == "production" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "endpoint not available")
		return
	}
	if h.jwtSecret == "" {
		writeError(w, http.StatusInternalServerError, "AUTH_MISCONFIGURED", "JWT secret is not configured")
		return
	}

	var req devTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if req.UserID == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "user_id and email are required")
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER_ID", "user_id must be a valid UUID")
		return
	}

	user, err := h.users.EnsureUserByID(r.Context(), userID, req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "USER_ENSURE_FAILED", err.Error())
		return
	}

	parsedID, err := uuid.Parse(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INVALID_USER_ID", "stored user id is invalid")
		return
	}

	token, err := auth.GenerateToken(parsedID, user.Email, h.jwtSecret, h.tokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   int(h.tokenTTL.Seconds()),
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
			},
		},
	})
}
