package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/model"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// ImageHandler serves image upload endpoints.
type ImageHandler struct {
	images        *service.ImageService
	cdnBaseURL    string
	serverBaseURL string
}

// NewImageHandler constructs an ImageHandler.
func NewImageHandler(images *service.ImageService, cdnBaseURL, serverBaseURL string) *ImageHandler {
	return &ImageHandler{
		images:        images,
		cdnBaseURL:    strings.TrimRight(strings.TrimSpace(cdnBaseURL), "/"),
		serverBaseURL: strings.TrimRight(strings.TrimSpace(serverBaseURL), "/"),
	}
}

type uploadSuccessResponse struct {
	Success bool              `json:"success"`
	Data    uploadSuccessData `json:"data"`
}

type uploadSuccessData struct {
	ID           string              `json:"id"`
	Fingerprint  string              `json:"fingerprint"`
	OriginalURL  string              `json:"original_url"`
	Width        int                 `json:"width"`
	Height       int                 `json:"height"`
	Format       string              `json:"format"`
	SizeBytes    int64               `json:"size_bytes"`
	Status       string              `json:"status"`
	Deduplicated bool                `json:"deduplicated,omitempty"`
	Location     *uploadLocationData `json:"location,omitempty"`
	Variants     []uploadVariantData `json:"variants"`
}

type uploadLocationData struct {
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Altitude  *float64 `json:"altitude,omitempty"`
	Source    string   `json:"source"`
}

type uploadVariantData struct {
	ID        string `json:"id"`
	Preset    string `json:"preset_name"`
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SizeBytes int64  `json:"size_bytes"`
	MimeType  string `json:"mime_type"`
}

type uploadErrorResponse struct {
	Success bool            `json:"success"`
	Error   uploadErrorBody `json:"error"`
}

type uploadErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// UploadHandler handles POST /api/v1/images.
func (h *ImageHandler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "invalid multipart form")
		return
	}

	_, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_IMAGE", "multipart field \"image\" is required")
		return
	}

	result, err := h.images.ProcessAndUpload(
		r.Context(),
		header,
		middleware.GetUserID(r.Context()),
		middleware.GetGuestSessionID(r.Context()),
		service.ParseOptionalFloat(r.FormValue("latitude")),
		service.ParseOptionalFloat(r.FormValue("longitude")),
	)
	if err != nil {
		status, code := mapUploadError(err)
		writeError(w, status, code, err.Error())
		return
	}

	if result.GuestSessionID != "" {
		w.Header().Set("X-Guest-Session-ID", result.GuestSessionID)
	}

	img := result.Image
	variants := make([]uploadVariantData, 0, len(result.Variants))
	for _, variant := range result.Variants {
		variants = append(variants, uploadVariantData{
			ID:        variant.ID,
			Preset:    variant.PresetName,
			URL:       h.variantPublicURL(r, img.ID, variant),
			Width:     variant.Width,
			Height:    variant.Height,
			SizeBytes: variant.FileSizeBytes,
			MimeType:  variant.MimeType,
		})
	}

	statusCode := http.StatusCreated
	if result.Deduplicated {
		statusCode = http.StatusOK
	}

	writeJSON(w, statusCode, uploadSuccessResponse{
		Success: true,
		Data: uploadSuccessData{
			ID:           img.ID,
			Fingerprint:  img.Fingerprint,
			OriginalURL:  h.images.ObjectURL(img),
			Width:        intOrZero(img.Width),
			Height:       intOrZero(img.Height),
			Format:       formatFromMIME(img.MimeType),
			SizeBytes:    img.FileSizeBytes,
			Status:       img.Status,
			Deduplicated: result.Deduplicated,
			Location:     locationPayload(img),
			Variants:     variants,
		},
	})
}

// GetVariantHandler streams a stored variant from S3 via the API.
// Also known as ServeVariantHandler for CDN/proxy edge caching.
func (h *ImageHandler) GetVariantHandler(w http.ResponseWriter, r *http.Request) {
	h.ServeVariantHandler(w, r)
}

// ServeVariantHandler streams a stored variant from S3 with long-lived cache headers.
func (h *ImageHandler) ServeVariantHandler(w http.ResponseWriter, r *http.Request) {
	imageID := r.PathValue("id")
	preset := r.PathValue("preset")
	if imageID == "" || preset == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "image id and preset are required")
		return
	}

	variant, err := h.images.FindVariantByPreset(r.Context(), imageID, preset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VARIANT_LOOKUP_FAILED", err.Error())
		return
	}
	if variant == nil {
		writeError(w, http.StatusNotFound, "VARIANT_NOT_FOUND", "variant not found")
		return
	}

	etag := fmt.Sprintf(`W/"%s"`, variant.ID)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body, contentType, err := h.images.OpenVariantObject(r.Context(), variant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VARIANT_FETCH_FAILED", err.Error())
		return
	}
	defer body.Close()

	if contentType == "" || contentType == "application/octet-stream" {
		contentType = "image/webp"
	}
	if variant.MimeType != "" {
		contentType = variant.MimeType
	}
	if strings.HasSuffix(strings.ToLower(variant.StoragePath), ".webp") {
		contentType = "image/webp"
	}

	filename := preset + ".webp"
	if strings.HasSuffix(strings.ToLower(variant.StoragePath), ".jpg") ||
		strings.HasSuffix(strings.ToLower(variant.StoragePath), ".jpeg") {
		filename = preset + ".jpg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (h *ImageHandler) variantPublicURL(r *http.Request, imageID string, variant *model.ImageVariant) string {
	if h.cdnBaseURL != "" && variant != nil && variant.StoragePath != "" {
		return h.cdnBaseURL + "/" + strings.TrimPrefix(variant.StoragePath, "/")
	}

	path := fmt.Sprintf("/api/v1/images/%s/variants/%s", imageID, variant.PresetName)
	if h.serverBaseURL != "" {
		return h.serverBaseURL + path
	}
	return absoluteURL(r, path)
}

func absoluteURL(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		host = "localhost:8080"
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

func mapUploadError(err error) (int, string) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "10MB"):
		return http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE"
	case strings.Contains(msg, "unsupported content type"):
		return http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"
	case strings.Contains(msg, "empty file"), strings.Contains(msg, "file is required"):
		return http.StatusBadRequest, "INVALID_IMAGE"
	case strings.Contains(msg, "generate variants"), strings.Contains(msg, "decode image"):
		return http.StatusUnprocessableEntity, "VARIANT_GENERATION_FAILED"
	default:
		return http.StatusInternalServerError, "UPLOAD_FAILED"
	}
}

func formatFromMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	default:
		parts := strings.Split(mimeType, "/")
		if len(parts) == 2 {
			return parts[1]
		}
		return mimeType
	}
}

func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func locationPayload(img *model.Image) *uploadLocationData {
	if img == nil || img.Latitude == nil || img.Longitude == nil {
		return nil
	}
	source := ""
	if img.LocationSource != nil {
		source = *img.LocationSource
	}
	return &uploadLocationData{
		Latitude:  *img.Latitude,
		Longitude: *img.Longitude,
		Altitude:  img.Altitude,
		Source:    source,
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, uploadErrorResponse{
		Success: false,
		Error: uploadErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
