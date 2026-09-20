package handler

import (
	"encoding/json"
	"net/http"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// UserHandler serves authenticated user endpoints.
type UserHandler struct {
	images *service.ImageService
}

// NewUserHandler constructs a UserHandler.
func NewUserHandler(images *service.ImageService) *UserHandler {
	return &UserHandler{images: images}
}

type claimGuestMediaRequest struct {
	GuestSessionID string `json:"guest_session_id"`
}

// ClaimGuestMediaHandler converts guest ephemeral media into permanent user assets.
func (h *UserHandler) ClaimGuestMediaHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	var req claimGuestMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if req.GuestSessionID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_GUEST_SESSION", "guest_session_id is required")
		return
	}

	claimed, err := h.images.ClaimGuestMedia(r.Context(), *userID, req.GuestSessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CLAIM_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"claimed_count": claimed,
			"message":       "Guest media converted to permanent user assets.",
		},
	})
}
