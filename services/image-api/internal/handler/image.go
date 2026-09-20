package handler

import (
	"encoding/json"
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
	ID          string              `json:"id"`
	Fingerprint string              `json:"fingerprint"`
	OriginalURL string              `json:"original_url"`
	Width       int                 `json:"width"`
	Height      int                 `json:"height"`
	Format      string              `json:"format"`
	SizeBytes   int64               `json:"size_bytes"`
	Status      string              `json:"status"`
	Variants    []uploadVariantData `json:"variants"`
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
			URL:       h.images.VariantURL(variant),
			Width:     variant.Width,
			Height:    variant.Height,
			SizeBytes: variant.FileSizeBytes,
			MimeType:  variant.MimeType,
		})
	}

	writeJSON(w, http.StatusCreated, uploadSuccessResponse{
		Success: true,
		Data: uploadSuccessData{
			ID:          img.ID,
			Fingerprint: img.Fingerprint,
			OriginalURL: h.images.ObjectURL(img),
			Width:       intOrZero(img.Width),
			Height:      intOrZero(img.Height),
			Format:      formatFromMIME(img.MimeType),
			SizeBytes:   img.FileSizeBytes,
			Status:      img.Status,
			Variants:    variants,
		},
	})
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
