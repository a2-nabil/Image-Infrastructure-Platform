package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/service"
)

const galleryChunkMaxBody = (5 << 20) + (1 << 20) // 5MiB chunk + slack

// GalleryHandler serves auth-only chunked uploads and albums.
type GalleryHandler struct {
	images *service.ImageService
}

func NewGalleryHandler(images *service.ImageService) *GalleryHandler {
	return &GalleryHandler{images: images}
}

type initUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	TotalSize   int64  `json:"total_size"`
	AlbumID     string `json:"album_id,omitempty"`
}

type albumWriteRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type albumItemsRequest struct {
	IDs []string `json:"ids"`
}

// InitUploadHandler starts a chunked multipart upload (dashboard / auth only).
func (h *GalleryHandler) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var req initUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	in := service.GalleryInitInput{
		Filename:    req.Filename,
		ContentType: req.ContentType,
		TotalSize:   req.TotalSize,
	}
	if strings.TrimSpace(req.AlbumID) != "" {
		aid, err := uuid.Parse(req.AlbumID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
			return
		}
		in.AlbumID = &aid
	}
	result, err := h.images.InitGalleryUpload(r.Context(), *userID, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, "UPLOAD_INIT_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data": map[string]any{
			"upload_id":       result.Session.ID,
			"chunk_size":      result.ChunkSize,
			"total_chunks":    result.TotalChunks,
			"media_kind":      result.Session.MediaKind,
			"expires_at":      result.ExpiresAt,
			"received_chunks": result.Session.ReceivedChunks,
		},
	})
}

// PutChunkHandler accepts one chunk (1-based index) for an upload session.
func (h *GalleryHandler) PutChunkHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	uploadID := r.PathValue("id")
	chunkRaw := r.PathValue("index")
	chunkIndex, err := strconv.Atoi(chunkRaw)
	if err != nil || chunkIndex < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "chunk index must be a positive integer")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, galleryChunkMaxBody)
	payload, err := io.ReadAll(io.LimitReader(r.Body, galleryChunkMaxBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "failed to read chunk body")
		return
	}
	if len(payload) == 0 || len(payload) > galleryChunkMaxBody {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "invalid chunk size")
		return
	}
	session, err := h.images.PutGalleryChunk(
		r.Context(),
		*userID,
		uploadID,
		chunkIndex,
		bytes.NewReader(payload),
		int64(len(payload)),
	)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "CHUNK_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"upload_id":       session.ID,
			"received_chunks": session.ReceivedChunks,
			"total_chunks":    session.TotalChunks,
			"status":          session.Status,
		},
	})
}

// CompleteUploadHandler finalizes multipart upload and stores the original only (no WebP).
func (h *GalleryHandler) CompleteUploadHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	uploadID := r.PathValue("id")
	img, err := h.images.CompleteGalleryUpload(r.Context(), *userID, uploadID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "UPLOAD_COMPLETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"id":                img.ID,
			"status":            img.Status,
			"media_kind":        img.MediaKind,
			"original_filename": img.OriginalFilename,
			"mime_type":         img.MimeType,
			"file_size_bytes":   img.FileSizeBytes,
			"fingerprint":       img.Fingerprint,
		},
	})
}

// AbortUploadHandler cancels an in-progress upload.
func (h *GalleryHandler) AbortUploadHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	if err := h.images.AbortGalleryUpload(r.Context(), *userID, r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, "UPLOAD_ABORT_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]any{"aborted": true},
	})
}

// ListAlbumsHandler lists the user's albums.
func (h *GalleryHandler) ListAlbumsHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	albums, err := h.images.ListAlbums(r.Context(), *userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ALBUM_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": albums})
}

// CreateAlbumHandler creates an album.
func (h *GalleryHandler) CreateAlbumHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	var req albumWriteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	album, err := h.images.CreateAlbum(r.Context(), *userID, req.Title, req.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ALBUM_CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "data": album})
}

// UpdateAlbumHandler renames an album.
func (h *GalleryHandler) UpdateAlbumHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	albumID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
		return
	}
	var req albumWriteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	album, err := h.images.RenameAlbum(r.Context(), *userID, albumID, req.Title, req.Description)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ALBUM_UPDATE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": album})
}

// DeleteAlbumHandler deletes an album (media remains in library).
func (h *GalleryHandler) DeleteAlbumHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	albumID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
		return
	}
	if err := h.images.DeleteAlbum(r.Context(), *userID, albumID); err != nil {
		writeError(w, http.StatusBadRequest, "ALBUM_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"deleted": true}})
}

// AddAlbumItemsHandler adds media to an album.
func (h *GalleryHandler) AddAlbumItemsHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	albumID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
		return
	}
	var req albumItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if err := h.images.AddAlbumItems(r.Context(), *userID, albumID, req.IDs); err != nil {
		writeError(w, http.StatusBadRequest, "ALBUM_ADD_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"added": true}})
}

// RemoveAlbumItemsHandler removes media from an album.
func (h *GalleryHandler) RemoveAlbumItemsHandler(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	}
	albumID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ALBUM", "invalid album id")
		return
	}
	var req albumItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if err := h.images.RemoveAlbumItems(r.Context(), *userID, albumID, req.IDs); err != nil {
		writeError(w, http.StatusBadRequest, "ALBUM_REMOVE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"removed": true}})
}
