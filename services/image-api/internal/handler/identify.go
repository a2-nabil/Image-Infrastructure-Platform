package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// IdentifyHandler serves identification cache/persist endpoints.
type IdentifyHandler struct {
	identify *service.IdentifyService
}

func NewIdentifyHandler(identify *service.IdentifyService) *IdentifyHandler {
	return &IdentifyHandler{identify: identify}
}

type identifyLookupRequest struct {
	Fingerprint string    `json:"fingerprint"`
	Embedding   []float64 `json:"embedding,omitempty"`
}

type identifyStoreRequest struct {
	Fingerprint      string    `json:"fingerprint"`
	ImageID          string    `json:"image_id,omitempty"`
	ObjectName       string    `json:"object_name"`
	ConfidenceScore  float64   `json:"confidence_score"`
	Summary          string    `json:"summary"`
	KeyDetails       []string  `json:"key_details"`
	SuggestedActions []string  `json:"suggested_actions"`
	Model            string    `json:"model,omitempty"`
	Embedding        []float64 `json:"embedding,omitempty"`
	CacheSource      string    `json:"cache_source,omitempty"`
}

// LookupHandler checks Redis exact → DB exact → semantic embedding cache.
func (h *IdentifyHandler) LookupHandler(w http.ResponseWriter, r *http.Request) {
	var req identifyLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Fingerprint) == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FINGERPRINT", "fingerprint is required")
		return
	}

	result, err := h.identify.Lookup(r.Context(), req.Fingerprint, req.Embedding)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IDENTIFY_LOOKUP_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    result,
	})
}

// StoreHandler persists an identify result and warms Redis exact cache.
func (h *IdentifyHandler) StoreHandler(w http.ResponseWriter, r *http.Request) {
	var req identifyStoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if strings.TrimSpace(req.Fingerprint) == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FINGERPRINT", "fingerprint is required")
		return
	}

	var imageID *string
	if strings.TrimSpace(req.ImageID) != "" {
		id := strings.TrimSpace(req.ImageID)
		imageID = &id
	}

	row, err := h.identify.Store(
		r.Context(),
		req.Fingerprint,
		imageID,
		middleware.GetUserID(r.Context()),
		service.IdentifyPayload{
			ObjectName:       req.ObjectName,
			ConfidenceScore:  req.ConfidenceScore,
			Summary:          req.Summary,
			KeyDetails:       req.KeyDetails,
			SuggestedActions: req.SuggestedActions,
			Model:            req.Model,
			Embedding:        req.Embedding,
		},
		req.CacheSource,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IDENTIFY_STORE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data":    row,
	})
}
