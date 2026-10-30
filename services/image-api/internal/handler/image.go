package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"image-infrastructure-platform/services/image-api/internal/metrics"
	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/model"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// ImageHandler serves image upload endpoints.
type ImageHandler struct {
	images        *service.ImageService
	cdnBaseURL    string
	serverBaseURL string
	usePresign    bool
	presignTTL    time.Duration
	accessSigner  *service.VariantAccessSigner
	secureCookies bool
}

// NewImageHandler constructs an ImageHandler.
func NewImageHandler(
	images *service.ImageService,
	cdnBaseURL, serverBaseURL string,
	usePresign bool,
	presignTTLSeconds int,
	accessSigner *service.VariantAccessSigner,
	secureCookies bool,
) *ImageHandler {
	ttl := time.Duration(presignTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &ImageHandler{
		images:        images,
		cdnBaseURL:    strings.TrimRight(strings.TrimSpace(cdnBaseURL), "/"),
		serverBaseURL: strings.TrimRight(strings.TrimSpace(serverBaseURL), "/"),
		usePresign:    usePresign,
		presignTTL:    ttl,
		accessSigner:  accessSigner,
		secureCookies: secureCookies,
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
	// Cap the full request body before multipart parsing to reduce DoS risk.
	// Slightly above the 10MB file limit to allow multipart framing overhead.
	const maxBodyBytes = (10 << 20) + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "invalid multipart form")
		return
	}

	_, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_IMAGE", "multipart field \"image\" is required")
		return
	}

	presets := splitCSV(r.FormValue("presets"))
	quality := 0
	if rawQ := strings.TrimSpace(r.FormValue("quality")); rawQ != "" {
		if n, err := strconv.Atoi(rawQ); err == nil {
			quality = n
		}
	}
	customWidth := 0
	customHeight := 0
	if rawW := strings.TrimSpace(r.FormValue("width")); rawW != "" {
		if n, err := strconv.Atoi(rawW); err == nil {
			customWidth = n
		}
	}
	if rawH := strings.TrimSpace(r.FormValue("height")); rawH != "" {
		if n, err := strconv.Atoi(rawH); err == nil {
			customHeight = n
		}
	}
	if customWidth > 0 || customHeight > 0 {
		presets = append(presets, "custom")
	}

	result, err := h.images.ProcessAndUpload(
		r.Context(),
		header,
		middleware.GetUserID(r.Context()),
		middleware.GetGuestSessionID(r.Context()),
		service.ParseOptionalFloat(r.FormValue("latitude")),
		service.ParseOptionalFloat(r.FormValue("longitude")),
		service.ConvertOptions{
			Presets:      presets,
			Quality:      quality,
			CustomWidth:  customWidth,
			CustomHeight: customHeight,
		},
	)
	if err != nil {
		status, code := mapUploadError(err)
		writeError(w, status, code, err.Error())
		return
	}

	metrics.Default.IncUpload(result.Deduplicated)

	if result.GuestSessionID != "" {
		w.Header().Set("X-Guest-Session-ID", result.GuestSessionID)
		h.setGuestSessionCookie(w, result.GuestSessionID)
	}

	img := result.Image
	variants := make([]uploadVariantData, 0, len(result.Variants))
	for _, variant := range result.Variants {
		variants = append(variants, uploadVariantData{
			ID:        variant.ID,
			Preset:    variant.PresetName,
			URL:       h.variantPublicURL(r, img, variant),
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
			OriginalURL:  h.originalPublicURL(r, img),
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

// GetImageHandler returns image metadata + variants for async status polling.
func (h *ImageHandler) GetImageHandler(w http.ResponseWriter, r *http.Request) {
	imageID := r.PathValue("id")
	if imageID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "image id is required")
		return
	}

	img, variants, err := h.images.GetImageWithVariants(r.Context(), imageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_LOOKUP_FAILED", err.Error())
		return
	}
	if img == nil {
		writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	if !service.CanAccessImage(img, middleware.GetUserID(r.Context()), middleware.GetGuestSessionID(r.Context())) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "you do not have access to this image")
		return
	}

	out := make([]uploadVariantData, 0, len(variants))
	for _, variant := range variants {
		out = append(out, uploadVariantData{
			ID:        variant.ID,
			Preset:    variant.PresetName,
			URL:       h.variantPublicURL(r, img, variant),
			Width:     variant.Width,
			Height:    variant.Height,
			SizeBytes: variant.FileSizeBytes,
			MimeType:  variant.MimeType,
		})
	}

	writeJSON(w, http.StatusOK, uploadSuccessResponse{
		Success: true,
		Data: uploadSuccessData{
			ID:          img.ID,
			Fingerprint: img.Fingerprint,
			OriginalURL: h.originalPublicURL(r, img),
			Width:       intOrZero(img.Width),
			Height:      intOrZero(img.Height),
			Format:      formatFromMIME(img.MimeType),
			SizeBytes:   img.FileSizeBytes,
			Status:      img.Status,
			Location:    locationPayload(img),
			Variants:    out,
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

	img, err := h.images.FindImageByID(r.Context(), imageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_LOOKUP_FAILED", err.Error())
		return
	}
	if img == nil {
		writeError(w, http.StatusNotFound, "VARIANT_NOT_FOUND", "variant not found")
		return
	}
	if !h.canAccessVariant(r, img) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "you do not have access to this variant")
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
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
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

	filename := webpDownloadName(img.OriginalFilename, variant.Width, variant.Height, intOrZero(img.Width), intOrZero(img.Height))

	disposition := "inline"
	if strings.EqualFold(r.URL.Query().Get("download"), "1") {
		disposition = "attachment"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (h *ImageHandler) originalPublicURL(r *http.Request, img *model.Image) string {
	if img == nil {
		return ""
	}
	// Always expose the API proxy path (not a raw S3 URL) so browsers/gallery use /api/v1.
	path := fmt.Sprintf("/api/v1/images/%s/original", img.ID)
	if q := h.accessQuery(img); q != "" {
		path += q
	}
	if h.serverBaseURL != "" {
		return h.serverBaseURL + path
	}
	return absoluteURL(r, path)
}

// ServeOriginalHandler streams the original upload from S3 via the API proxy.
func (h *ImageHandler) ServeOriginalHandler(w http.ResponseWriter, r *http.Request) {
	imageID := r.PathValue("id")
	if imageID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "image id is required")
		return
	}

	img, err := h.images.FindImageByID(r.Context(), imageID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_LOOKUP_FAILED", err.Error())
		return
	}
	if img == nil {
		writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	if !h.canAccessVariant(r, img) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "you do not have access to this image")
		return
	}

	etag := fmt.Sprintf(`W/"original-%s-%d"`, img.ID, img.UpdatedAt.Unix())
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body, contentType, err := h.images.OpenOriginalObject(r.Context(), img)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ORIGINAL_FETCH_FAILED", err.Error())
		return
	}
	defer body.Close()

	if contentType == "" || contentType == "application/octet-stream" {
		contentType = img.MimeType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	filename := img.OriginalFilename
	if filename == "" {
		filename = "original"
	}

	disposition := "inline"
	if strings.EqualFold(r.URL.Query().Get("download"), "1") {
		disposition = "attachment"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=%q", disposition, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (h *ImageHandler) variantPublicURL(r *http.Request, img *model.Image, variant *model.ImageVariant) string {
	if variant == nil || img == nil {
		return ""
	}
	// Never expose S3/presigned/CDN object URLs to clients — always API proxy paths.
	path := fmt.Sprintf("/api/v1/images/%s/variants/%s", img.ID, variant.PresetName)
	if q := h.accessQuery(img); q != "" {
		path += q
	}
	if h.serverBaseURL != "" {
		return h.serverBaseURL + path
	}
	return absoluteURL(r, path)
}

func (h *ImageHandler) accessQuery(img *model.Image) string {
	if h == nil || h.accessSigner == nil || !h.accessSigner.Enabled() || img == nil {
		return ""
	}
	tenant := service.ImageTenantKey(img)
	if tenant == "" {
		return ""
	}
	return h.accessSigner.Query(img.ID, tenant)
}

func (h *ImageHandler) canAccessVariant(r *http.Request, img *model.Image) bool {
	if service.CanAccessImage(img, middleware.GetUserID(r.Context()), middleware.GetGuestSessionID(r.Context())) {
		return true
	}
	// Capability URL: ?exp=&sig= lets <img src> / downloads work without custom headers.
	// Signature is tenant-bound so another account's leaked URL cannot authorize this object
	// unless it was issued for the same owner scope.
	if h.accessSigner == nil || !h.accessSigner.Enabled() || img == nil {
		return false
	}
	tenant := service.ImageTenantKey(img)
	if tenant == "" {
		return false
	}
	expRaw := strings.TrimSpace(r.URL.Query().Get("exp"))
	sig := strings.TrimSpace(r.URL.Query().Get("sig"))
	if expRaw == "" || sig == "" {
		return false
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		return false
	}
	return h.accessSigner.Verify(img.ID, tenant, sig, exp)
}

func (h *ImageHandler) setGuestSessionCookie(w http.ResponseWriter, guestSessionID string) {
	if guestSessionID == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.GuestSessionCookie,
		Value:    guestSessionID,
		Path:     "/",
		MaxAge:   int((72 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
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

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fileBaseName(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		return "image"
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i]
	}
	return name
}

// webpDownloadName uses the uploaded basename. Appends -WxH only when dimensions change.
func webpDownloadName(originalFilename string, width, height, sourceW, sourceH int) string {
	base := fileBaseName(originalFilename)
	if width > 0 && height > 0 && sourceW > 0 && sourceH > 0 &&
		(width != sourceW || height != sourceH) {
		return fmt.Sprintf("%s-%dx%d.webp", base, width, height)
	}
	return base + ".webp"
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
