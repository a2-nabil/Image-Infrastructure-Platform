package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/model"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// UserHandler serves authenticated user endpoints.
type UserHandler struct {
	images        *service.ImageService
	users         *service.UserService
	imagesURL     *ImageHandler
	identify      *service.IdentifyService
	galleryAccess *service.GalleryAccessService
}

// NewUserHandler constructs a UserHandler.
func NewUserHandler(
	images *service.ImageService,
	users *service.UserService,
	imagesURL *ImageHandler,
	identify *service.IdentifyService,
	galleryAccess *service.GalleryAccessService,
) *UserHandler {
	return &UserHandler{
		images:        images,
		users:         users,
		imagesURL:     imagesURL,
		identify:      identify,
		galleryAccess: galleryAccess,
	}
}

type claimGuestMediaRequest struct {
	GuestSessionID string `json:"guest_session_id"`
}

type imageIDsRequest struct {
	IDs []string `json:"ids"`
}

// MeHandler returns the authenticated user profile.
func (h *UserHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	user, err := h.users.GetByID(r.Context(), *userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "USER_LOOKUP_FAILED", err.Error())
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}

	payload := userPayload(user)
	isSuper := false
	galleryOK := false
	if h.galleryAccess != nil {
		isSuper = h.galleryAccess.IsSuperAdmin(user.Email)
		ok, err := h.galleryAccess.CanAccessGallery(r.Context(), user.Email)
		if err == nil {
			galleryOK = ok
		}
	}
	payload["is_super_admin"] = isSuper
	payload["gallery_access"] = galleryOK

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    payload,
	})
}

// ListMyImagesHandler returns Converter / Identify / Dashboard tools library only.
func (h *UserHandler) ListMyImagesHandler(w http.ResponseWriter, r *http.Request) {
	h.writeOwnedImageList(w, r, model.LibraryScopeTools)
}

// ListGalleryMediaHandler returns personal gallery media only (requireGallery).
func (h *UserHandler) ListGalleryMediaHandler(w http.ResponseWriter, r *http.Request) {
	h.writeOwnedImageList(w, r, model.LibraryScopeGallery)
}

func (h *UserHandler) writeOwnedImageList(w http.ResponseWriter, r *http.Request, libraryScope string) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}

	limit := 24
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}
	trashOnly := r.URL.Query().Get("trash") == "1" || r.URL.Query().Get("trash") == "true"
	filter := service.ListImagesFilter{
		TrashOnly:    trashOnly,
		MediaKind:    r.URL.Query().Get("media_kind"),
		LibraryScope: libraryScope,
	}
	if rawAlbum := strings.TrimSpace(r.URL.Query().Get("album_id")); rawAlbum != "" {
		if libraryScope != model.LibraryScopeGallery {
			writeError(w, http.StatusBadRequest, "ALBUMS_GALLERY_ONLY", "albums are only available in personal gallery")
			return
		}
		aid, err := uuid.Parse(rawAlbum)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
			return
		}
		filter.AlbumID = &aid
	}

	bundles, total, err := h.images.ListImagesForUserFiltered(r.Context(), *userID, limit, offset, filter)
	if err != nil {
		if strings.Contains(err.Error(), "album not found") {
			writeError(w, http.StatusNotFound, "ALBUM_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "IMAGE_LIST_FAILED", err.Error())
		return
	}

	items := make([]map[string]any, 0, len(bundles))
	var totalBytes int64
	var variantCount int
	statusCounts := map[string]int{}

	for _, bundle := range bundles {
		img := bundle.Image
		if img == nil {
			continue
		}
		statusCounts[img.Status]++
		totalBytes += img.FileSizeBytes
		variantCount += len(bundle.Variants)

		variants := make([]uploadVariantData, 0, len(bundle.Variants))
		for _, variant := range bundle.Variants {
			url := ""
			if h.imagesURL != nil {
				url = h.imagesURL.variantPublicURL(r, img, variant)
			}
			variants = append(variants, uploadVariantData{
				ID:        variant.ID,
				Preset:    variant.PresetName,
				URL:       url,
				Width:     variant.Width,
				Height:    variant.Height,
				SizeBytes: variant.FileSizeBytes,
				MimeType:  variant.MimeType,
			})
			totalBytes += variant.FileSizeBytes
		}

		originalURL := ""
		if h.imagesURL != nil {
			originalURL = h.imagesURL.originalPublicURL(r, img)
		}

		var identification any
		if h.identify != nil {
			if idRow, err := h.identify.GetByImageID(r.Context(), img.ID); err == nil && idRow != nil {
				identification = idRow
			}
		}

		mediaKind := img.MediaKind
		if mediaKind == "" {
			mediaKind = model.MediaKindImage
		}
		item := map[string]any{
			"id":                img.ID,
			"fingerprint":       img.Fingerprint,
			"original_filename": img.OriginalFilename,
			// storage_path intentionally omitted — never expose S3 object keys/URLs to clients.
			"original_url":     originalURL,
			"mime_type":        img.MimeType,
			"format":           formatFromMIME(img.MimeType),
			"file_size_bytes":  img.FileSizeBytes,
			"width":            intOrZero(img.Width),
			"height":           intOrZero(img.Height),
			"status":           img.Status,
			"media_kind":       mediaKind,
			"duration_ms":      img.DurationMs,
			"user_id":          img.UserID,
			"guest_session_id": img.GuestSessionID,
			"expires_at":       img.ExpiresAt,
			"trashed_at":       img.TrashedAt,
			"location":         locationPayload(img),
			"created_at":       img.CreatedAt,
			"updated_at":       img.UpdatedAt,
			"variants":         variants,
			"identification":   identification,
		}
		items = append(items, item)
	}

	nextOffset := offset + len(items)
	hasMore := nextOffset < total

	// Private browser/CDN cache hint for the signed-in gallery list.
	w.Header().Set("Cache-Control", "private, max-age=15, stale-while-revalidate=60")

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"items": items,
			"pagination": map[string]any{
				"limit":       limit,
				"offset":      offset,
				"next_offset": nextOffset,
				"total":       total,
				"has_more":    hasMore,
			},
			"summary": map[string]any{
				"image_count":      total,
				"variant_count":    variantCount,
				"total_size_bytes": totalBytes,
				"status_counts":    statusCounts,
			},
		},
	})
}

// DeleteMyImageHandler moves one owned image to trash (soft delete).
func (h *UserHandler) DeleteMyImageHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	imageID := r.PathValue("id")
	if imageID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "image id is required")
		return
	}

	n, err := h.images.TrashOwnedImages(r.Context(), *userID, []string{imageID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_TRASH_FAILED", err.Error())
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"id":      imageID,
			"trashed": true,
			"count":   n,
		},
	})
}

// TrashMyImagesHandler moves many owned images to trash.
func (h *UserHandler) TrashMyImagesHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var req imageIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	n, err := h.images.TrashOwnedImages(r.Context(), *userID, req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_TRASH_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"trashed": true, "count": n},
	})
}

// RestoreMyImagesHandler restores images from trash.
func (h *UserHandler) RestoreMyImagesHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var req imageIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	n, err := h.images.RestoreOwnedImages(r.Context(), *userID, req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_RESTORE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"restored": true, "count": n},
	})
}

// PurgeMyImagesHandler permanently deletes images from DB and S3.
func (h *UserHandler) PurgeMyImagesHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var req imageIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	n, err := h.images.PurgeOwnedImages(r.Context(), *userID, req.IDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "IMAGE_PURGE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"purged": true, "count": n},
	})
}

// DeleteMyVariantHandler deletes one owned WebP variant from S3 and the database.
func (h *UserHandler) DeleteMyVariantHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	imageID := r.PathValue("id")
	preset := r.PathValue("preset")
	if imageID == "" || preset == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "image id and preset are required")
		return
	}

	err := h.images.DeleteOwnedVariant(r.Context(), *userID, imageID, preset)
	if errors.Is(err, service.ErrOwnedImageNotFound) {
		writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
		return
	}
	if errors.Is(err, service.ErrOwnedVariantNotFound) {
		writeError(w, http.StatusNotFound, "VARIANT_NOT_FOUND", "variant not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VARIANT_DELETE_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"id":      imageID,
			"preset":  preset,
			"deleted": true,
			"scope":   "variant",
		},
	})
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
	// Only claim the caller's own guest session (header/cookie). Body alone cannot
	// steal another browser's ephemeral uploads.
	boundGuest := strings.TrimSpace(middleware.GetGuestSessionID(r.Context()))
	requested := strings.TrimSpace(req.GuestSessionID)
	if boundGuest == "" {
		writeError(w, http.StatusBadRequest, "MISSING_GUEST_SESSION", "guest session header is required")
		return
	}
	if requested != "" && requested != boundGuest {
		writeError(w, http.StatusForbidden, "GUEST_SESSION_MISMATCH", "guest_session_id does not match this browser session")
		return
	}

	result, err := h.images.ClaimGuestMedia(r.Context(), *userID, boundGuest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CLAIM_FAILED", err.Error())
		return
	}
	if result == nil {
		result = &service.ClaimGuestResult{}
	}

	message := "Guest media converted to permanent user assets."
	if result.Merged > 0 && result.Claimed == 0 {
		message = "Guest media matched existing library fingerprints and was merged (no duplicates added)."
	} else if result.Merged > 0 {
		message = "Guest media claimed; duplicate fingerprints were merged into existing library items."
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"claimed_count": result.Claimed,
			"merged_count":  result.Merged,
			"message":       message,
		},
	})
}

func userPayload(user *model.User) map[string]any {
	if user == nil {
		return nil
	}
	return map[string]any{
		"id":         user.ID,
		"google_id":  user.GoogleID,
		"email":      user.Email,
		"name":       user.Name,
		"avatar_url": user.AvatarURL,
		"created_at": user.CreatedAt,
	}
}
