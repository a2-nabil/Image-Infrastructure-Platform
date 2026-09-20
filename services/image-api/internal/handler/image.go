package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/service"
)

// ImageHandler serves image upload endpoints.
type ImageHandler struct {
	images *service.ImageService
}

// NewImageHandler constructs an ImageHandler.
func NewImageHandler(images *service.ImageService) *ImageHandler {
	return &ImageHandler{images: images}
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
	Variants     []uploadVariantData `json:"variants"`
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

	result, err := h.images.ProcessAndUpload(r.Context(), header)
	if err != nil {
		status, code := mapUploadError(err)
		writeError(w, status, code, err.Error())
		return
	}

	img := result.Image
	variants := make([]uploadVariantData, 0, len(result.Variants))
	for _, variant := range result.Variants {
		variants = append(variants, uploadVariantData{
			ID:        variant.ID,
			Preset:    variant.PresetName,
			URL:       absoluteURL(r, h.images.VariantProxyPath(img.ID, variant.PresetName)),
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
			Variants:     variants,
		},
	})
}

// GetVariantHandler streams a stored variant from S3 via the API.
func (h *ImageHandler) GetVariantHandler(w http.ResponseWriter, r *http.Request) {
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

	body, contentType, err := h.images.OpenVariantObject(r.Context(), variant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VARIANT_FETCH_FAILED", err.Error())
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", preset+".jpg"))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
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
