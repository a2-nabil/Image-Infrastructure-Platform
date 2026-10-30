package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// GalleryAccessHandler manages the gallery email allowlist (super admin only).
type GalleryAccessHandler struct {
	users         *service.UserService
	galleryAccess *service.GalleryAccessService
}

func NewGalleryAccessHandler(
	users *service.UserService,
	galleryAccess *service.GalleryAccessService,
) *GalleryAccessHandler {
	return &GalleryAccessHandler{users: users, galleryAccess: galleryAccess}
}

type allowGalleryEmailRequest struct {
	Email string `json:"email"`
	Note  string `json:"note"`
}

func (h *GalleryAccessHandler) requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return false
	}
	user, err := h.users.GetByID(r.Context(), *userID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return false
	}
	if h.galleryAccess == nil || !h.galleryAccess.IsSuperAdmin(user.Email) {
		writeError(w, http.StatusForbidden, "SUPER_ADMIN_REQUIRED", "super admin only")
		return false
	}
	return true
}

// ListAllowedEmailsHandler returns the gallery allowlist.
func (h *GalleryAccessHandler) ListAllowedEmailsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requireSuperAdmin(w, r) {
		return
	}
	entries, err := h.galleryAccess.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GALLERY_ACCESS_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"super_admin_email": h.galleryAccess.SuperAdminEmail(),
			"entries":           entries,
		},
	})
}

// AllowEmailHandler adds an email to the gallery allowlist.
func (h *GalleryAccessHandler) AllowEmailHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requireSuperAdmin(w, r) {
		return
	}
	userID := middleware.GetUserID(r.Context())
	var req allowGalleryEmailRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	entry, err := h.galleryAccess.Allow(r.Context(), *userID, req.Email, req.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, "GALLERY_ACCESS_ALLOW_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "data": entry})
}

// RevokeEmailHandler removes an email from the gallery allowlist.
func (h *GalleryAccessHandler) RevokeEmailHandler(w http.ResponseWriter, r *http.Request) {
	if !h.requireSuperAdmin(w, r) {
		return
	}
	var req allowGalleryEmailRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, "INVALID_EMAIL", "email is required")
		return
	}
	if err := h.galleryAccess.Revoke(r.Context(), email); err != nil {
		writeError(w, http.StatusBadRequest, "GALLERY_ACCESS_REVOKE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"revoked": true, "email": strings.ToLower(email)},
	})
}
