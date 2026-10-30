package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/model"
)

const (
	galleryChunkSizeBytes    = 5 << 20 // 5 MiB (S3 multipart minimum for non-final parts)
	galleryMaxImageBytes     = 100 << 20
	galleryMaxVideoBytes     = 2 << 30 // 2 GiB originals for phone/camera dumps
	galleryUploadTTL         = 24 * time.Hour
	galleryMaxFilenameLen    = 180
	galleryMaxPendingUploads = 100 // bulk / background sync sessions
	galleryDailyMaxFiles     = 1000
	galleryDailyMaxBytes     = 50 << 30 // 50 GiB / user / rolling 24h
)

var (
	safeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._\-\s()]+`)

	galleryImageMIME = map[string]string{
		"image/jpeg": model.MediaKindImage,
		"image/png":  model.MediaKindImage,
		"image/webp": model.MediaKindImage,
		"image/gif":  model.MediaKindImage,
		"image/heic": model.MediaKindImage,
		"image/heif": model.MediaKindImage,
	}
	galleryVideoMIME = map[string]string{
		"video/mp4":       model.MediaKindVideo,
		"video/webm":      model.MediaKindVideo,
		"video/quicktime": model.MediaKindVideo,
	}

	galleryExtByMIME = map[string][]string{
		"image/jpeg":      {".jpg", ".jpeg"},
		"image/png":       {".png"},
		"image/webp":      {".webp"},
		"image/gif":       {".gif"},
		"image/heic":      {".heic", ".heif"},
		"image/heif":      {".heic", ".heif"},
		"video/mp4":       {".mp4", ".m4v"},
		"video/webm":      {".webm"},
		"video/quicktime": {".mov", ".qt"},
	}
)

type GalleryInitInput struct {
	Filename    string
	ContentType string
	TotalSize   int64
	AlbumID     *uuid.UUID
}

type GalleryInitResult struct {
	Session     *model.UploadSession
	ChunkSize   int
	TotalChunks int
	ExpiresAt   time.Time
}

// InitGalleryUpload starts an auth-only chunked multipart upload to S3.
func (s *ImageService) InitGalleryUpload(ctx context.Context, userID uuid.UUID, in GalleryInitInput) (*GalleryInitResult, error) {
	if s == nil || s.db == nil || s.s3 == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	filename := sanitizeGalleryFilename(in.Filename)
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(in.ContentType, ";")[0]))
	kind, ok := galleryMediaKind(contentType)
	if !ok {
		return nil, fmt.Errorf("unsupported content type %q", contentType)
	}
	if !galleryFilenameMatchesMIME(filename, contentType) {
		return nil, fmt.Errorf("filename extension does not match content type")
	}
	maxBytes := int64(galleryMaxImageBytes)
	if kind == model.MediaKindVideo {
		maxBytes = galleryMaxVideoBytes
	}
	if in.TotalSize <= 0 || in.TotalSize > maxBytes {
		return nil, fmt.Errorf("file size must be between 1 and %d bytes for %s", maxBytes, kind)
	}

	pending, err := s.countPendingUploads(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pending >= galleryMaxPendingUploads {
		return nil, fmt.Errorf("too many pending uploads (max %d)", galleryMaxPendingUploads)
	}
	if err := s.enforceGalleryDailyQuota(ctx, userID, in.TotalSize); err != nil {
		return nil, err
	}

	if in.AlbumID != nil {
		owned, err := s.userOwnsAlbum(ctx, userID, *in.AlbumID)
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, fmt.Errorf("album not found")
		}
	}

	totalChunks := int((in.TotalSize + galleryChunkSizeBytes - 1) / galleryChunkSizeBytes)
	if totalChunks < 1 {
		totalChunks = 1
	}

	objectID := uuid.NewString()
	storagePath := fmt.Sprintf(
		"users/%s/gallery/%s/%s",
		userID.String(),
		objectID,
		filename,
	)

	uploadID, err := s.s3.CreateMultipartUpload(ctx, storagePath, contentType)
	if err != nil {
		return nil, err
	}

	expires := time.Now().UTC().Add(galleryUploadTTL)
	var albumArg any
	if in.AlbumID != nil {
		albumArg = in.AlbumID.String()
	}

	const insertSQL = `
		INSERT INTO upload_sessions (
			user_id, original_filename, content_type, media_kind,
			total_size_bytes, chunk_size_bytes, total_chunks, received_chunks,
			storage_path, s3_upload_id, part_etags, status, album_id, expires_at
		) VALUES (
			$1::uuid, $2, $3, $4,
			$5, $6, $7, 0,
			$8, $9, '{}'::jsonb, 'pending', $10, $11
		)
		RETURNING id, user_id, original_filename, content_type, media_kind,
		          total_size_bytes, chunk_size_bytes, total_chunks, received_chunks,
		          storage_path, s3_upload_id, part_etags::text, status,
		          album_id, image_id, expires_at, created_at, updated_at
	`
	session, err := scanUploadSession(s.db.QueryRowContext(
		ctx,
		insertSQL,
		userID.String(),
		filename,
		contentType,
		kind,
		in.TotalSize,
		galleryChunkSizeBytes,
		totalChunks,
		storagePath,
		uploadID,
		albumArg,
		expires,
	))
	if err != nil {
		_ = s.s3.AbortMultipartUpload(ctx, storagePath, uploadID)
		return nil, fmt.Errorf("create upload session: %w", err)
	}

	return &GalleryInitResult{
		Session:     session,
		ChunkSize:   galleryChunkSizeBytes,
		TotalChunks: totalChunks,
		ExpiresAt:   expires,
	}, nil
}

// PutGalleryChunk stores one 1-based chunk for an owned upload session.
func (s *ImageService) PutGalleryChunk(
	ctx context.Context,
	userID uuid.UUID,
	sessionID string,
	chunkIndex int,
	body io.Reader,
	declaredSize int64,
) (*model.UploadSession, error) {
	if s == nil || s.db == nil || s.s3 == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if chunkIndex < 1 {
		return nil, fmt.Errorf("chunk index must be >= 1")
	}
	session, err := s.getOwnedUploadSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != "pending" {
		return nil, fmt.Errorf("upload session is %s", session.Status)
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.AbortGalleryUpload(ctx, userID, sessionID)
		return nil, fmt.Errorf("upload session expired")
	}
	if chunkIndex > session.TotalChunks {
		return nil, fmt.Errorf("chunk index exceeds total chunks")
	}

	maxPart := int64(session.ChunkSizeBytes)
	if chunkIndex == session.TotalChunks {
		remainder := session.TotalSizeBytes % int64(session.ChunkSizeBytes)
		if remainder > 0 {
			maxPart = remainder
		}
	}
	if declaredSize <= 0 || declaredSize > maxPart {
		return nil, fmt.Errorf("invalid chunk size (expected up to %d bytes)", maxPart)
	}

	limited := io.LimitReader(body, declaredSize+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read chunk: %w", err)
	}
	if int64(len(payload)) != declaredSize {
		return nil, fmt.Errorf("chunk size mismatch")
	}

	etag, err := s.s3.UploadPart(
		ctx,
		session.StoragePath,
		session.S3UploadID,
		int32(chunkIndex),
		bytes.NewReader(payload),
		declaredSize,
	)
	if err != nil {
		return nil, err
	}

	// Merge this part atomically. Concurrent chunk workers must not overwrite
	// each other's etags (that caused "missing chunks (1/N)" under parallel PUT).
	patch, err := json.Marshal(map[string]string{strconv.Itoa(chunkIndex): etag})
	if err != nil {
		return nil, err
	}

	const updateSQL = `
		UPDATE upload_sessions
		SET part_etags = COALESCE(part_etags, '{}'::jsonb) || $1::jsonb,
		    received_chunks = (
		      SELECT COUNT(*)::int
		      FROM jsonb_object_keys(COALESCE(part_etags, '{}'::jsonb) || $1::jsonb)
		    ),
		    updated_at = NOW()
		WHERE id = $2::uuid
		  AND user_id = $3::uuid
		  AND status = 'pending'
		RETURNING id, user_id, original_filename, content_type, media_kind,
		          total_size_bytes, chunk_size_bytes, total_chunks, received_chunks,
		          storage_path, s3_upload_id, part_etags::text, status,
		          album_id, image_id, expires_at, created_at, updated_at
	`
	updated, err := scanUploadSession(s.db.QueryRowContext(
		ctx, updateSQL, string(patch), sessionID, userID.String(),
	))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("upload session is not pending")
	}
	return updated, err
}

// CompleteGalleryUpload assembles the original object in S3 and stores it in the
// personal gallery. No WebP / variant conversion — originals only.
func (s *ImageService) CompleteGalleryUpload(
	ctx context.Context,
	userID uuid.UUID,
	sessionID string,
) (*model.Image, error) {
	if s == nil || s.db == nil || s.s3 == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	session, err := s.getOwnedUploadSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status == "completed" && session.ImageID != nil {
		img, _, err := s.GetImageWithVariants(ctx, *session.ImageID)
		return img, err
	}
	if session.Status != "pending" {
		return nil, fmt.Errorf("upload session is %s", session.Status)
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.AbortGalleryUpload(ctx, userID, sessionID)
		return nil, fmt.Errorf("upload session expired")
	}
	if err := s.enforceGalleryDailyQuota(ctx, userID, 0); err != nil {
		return nil, err
	}

	// Prefer part_etags map size over received_chunks counter (legacy race-safe).
	etags, err := parsePartETags(session.PartETags)
	if err != nil {
		return nil, err
	}
	if len(etags) != session.TotalChunks {
		// Brief re-read in case the last concurrent chunk commit landed after get.
		time.Sleep(150 * time.Millisecond)
		session, err = s.getOwnedUploadSession(ctx, userID, sessionID)
		if err != nil {
			return nil, err
		}
		etags, err = parsePartETags(session.PartETags)
		if err != nil {
			return nil, err
		}
	}
	if len(etags) != session.TotalChunks {
		return nil, fmt.Errorf("missing chunks (%d/%d)", len(etags), session.TotalChunks)
	}
	parts := make([]types.CompletedPart, 0, len(etags))
	for k, v := range etags {
		n, convErr := strconv.Atoi(k)
		if convErr != nil {
			return nil, fmt.Errorf("invalid part index")
		}
		etag := v
		partNum := int32(n)
		parts = append(parts, types.CompletedPart{ETag: &etag, PartNumber: &partNum})
	}
	sort.Slice(parts, func(i, j int) bool {
		return awsInt32(parts[i].PartNumber) < awsInt32(parts[j].PartNumber)
	})

	// Claim session atomically to prevent double-complete races.
	claimed, err := scanUploadSession(s.db.QueryRowContext(ctx, `
		UPDATE upload_sessions
		SET status = 'assembling', updated_at = NOW()
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		  AND status = 'pending'
		RETURNING id, user_id, original_filename, content_type, media_kind,
		          total_size_bytes, chunk_size_bytes, total_chunks, received_chunks,
		          storage_path, s3_upload_id, part_etags::text, status,
		          album_id, image_id, expires_at, created_at, updated_at
	`, sessionID, userID.String()))
	if err == sql.ErrNoRows {
		session, err = s.getOwnedUploadSession(ctx, userID, sessionID)
		if err != nil {
			return nil, err
		}
		if session.Status == "completed" && session.ImageID != nil {
			img, _, getErr := s.GetImageWithVariants(ctx, *session.ImageID)
			return img, getErr
		}
		return nil, fmt.Errorf("upload session is %s", session.Status)
	}
	if err != nil {
		return nil, err
	}
	session = claimed

	if err := s.s3.CompleteMultipartUpload(ctx, session.StoragePath, session.S3UploadID, parts); err != nil {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE upload_sessions SET status = 'failed', updated_at = NOW()
			WHERE id = $1::uuid
		`, sessionID)
		return nil, err
	}

	fingerprint, sniffed, err := s.hashAndSniffObject(ctx, session.StoragePath)
	if err != nil {
		return nil, err
	}
	if !galleryMIMECompatible(session.ContentType, sniffed, session.MediaKind) {
		_ = s.s3.DeleteObject(ctx, session.StoragePath)
		_, _ = s.db.ExecContext(ctx, `
			UPDATE upload_sessions SET status = 'failed', updated_at = NOW()
			WHERE id = $1::uuid
		`, sessionID)
		return nil, fmt.Errorf("uploaded content does not match declared type")
	}

	// Dedupe only within personal gallery scope (never tools / converter library).
	existing, err := s.findReadyByFingerprint(ctx, fingerprint, strPtr(userID.String()), "", model.LibraryScopeGallery)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		_ = s.s3.DeleteObject(ctx, session.StoragePath)
		if albumUUID := parseUUIDPtr(session.AlbumID); albumUUID != nil {
			_ = s.AddAlbumItems(ctx, userID, *albumUUID, []string{existing.ID})
		}
		_, _ = s.db.ExecContext(ctx, `
			UPDATE upload_sessions
			SET status = 'completed', image_id = $1::uuid, updated_at = NOW()
			WHERE id = $2::uuid
		`, existing.ID, sessionID)
		return existing, nil
	}

	imageID := uuid.NewString()
	uid := userID.String()
	const insertImage = `
		INSERT INTO images (
			id, fingerprint, original_filename, storage_path, mime_type,
			file_size_bytes, status, media_kind, library_scope, user_id
		) VALUES (
			$1::uuid, $2, $3, $4, $5,
			$6, 'ready', $7, $8, $9::uuid
		)
		RETURNING id, fingerprint, original_filename, storage_path, mime_type,
		          file_size_bytes, width, height, status, media_kind, library_scope, duration_ms,
		          user_id, created_at, updated_at
	`
	img := &model.Image{}
	var owner sql.NullString
	var duration sql.NullInt64
	err = s.db.QueryRowContext(
		ctx,
		insertImage,
		imageID,
		fingerprint,
		session.OriginalFilename,
		session.StoragePath,
		session.ContentType,
		session.TotalSizeBytes,
		session.MediaKind,
		model.LibraryScopeGallery,
		uid,
	).Scan(
		&img.ID,
		&img.Fingerprint,
		&img.OriginalFilename,
		&img.StoragePath,
		&img.MimeType,
		&img.FileSizeBytes,
		&img.Width,
		&img.Height,
		&img.Status,
		&img.MediaKind,
		&img.LibraryScope,
		&duration,
		&owner,
		&img.CreatedAt,
		&img.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert gallery media: %w", err)
	}
	if owner.Valid {
		img.UserID = &owner.String
	}
	if duration.Valid {
		d := int(duration.Int64)
		img.DurationMs = &d
	}

	if albumUUID := parseUUIDPtr(session.AlbumID); albumUUID != nil {
		_ = s.AddAlbumItems(ctx, userID, *albumUUID, []string{img.ID})
	}

	_, _ = s.db.ExecContext(ctx, `
		UPDATE upload_sessions
		SET status = 'completed', image_id = $1::uuid, updated_at = NOW()
		WHERE id = $2::uuid
	`, img.ID, sessionID)

	return img, nil
}

// AbortGalleryUpload cancels multipart upload and marks the session aborted.
func (s *ImageService) AbortGalleryUpload(ctx context.Context, userID uuid.UUID, sessionID string) error {
	session, err := s.getOwnedUploadSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if session.Status == "completed" {
		return fmt.Errorf("cannot abort completed upload")
	}
	_ = s.s3.AbortMultipartUpload(ctx, session.StoragePath, session.S3UploadID)
	_, err = s.db.ExecContext(ctx, `
		UPDATE upload_sessions
		SET status = 'aborted', updated_at = NOW()
		WHERE id = $1::uuid AND user_id = $2::uuid
	`, sessionID, userID.String())
	return err
}

func (s *ImageService) getOwnedUploadSession(ctx context.Context, userID uuid.UUID, sessionID string) (*model.UploadSession, error) {
	const q = `
		SELECT id, user_id, original_filename, content_type, media_kind,
		       total_size_bytes, chunk_size_bytes, total_chunks, received_chunks,
		       storage_path, s3_upload_id, part_etags::text, status,
		       album_id, image_id, expires_at, created_at, updated_at
		FROM upload_sessions
		WHERE id = $1::uuid AND user_id = $2::uuid
		LIMIT 1
	`
	session, err := scanUploadSession(s.db.QueryRowContext(ctx, q, sessionID, userID.String()))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("upload session not found")
	}
	return session, err
}

func (s *ImageService) countPendingUploads(ctx context.Context, userID uuid.UUID) (int, error) {
	const q = `
		SELECT COUNT(*)
		FROM upload_sessions
		WHERE user_id = $1::uuid
		  AND status IN ('pending', 'assembling')
		  AND expires_at > NOW()
	`
	var n int
	err := s.db.QueryRowContext(ctx, q, userID.String()).Scan(&n)
	return n, err
}

func (s *ImageService) enforceGalleryDailyQuota(ctx context.Context, userID uuid.UUID, extraBytes int64) error {
	const q = `
		SELECT COUNT(*)::bigint,
		       COALESCE(SUM(file_size_bytes), 0)::bigint
		FROM images
		WHERE user_id = $1::uuid
		  AND created_at >= NOW() - INTERVAL '24 hours'
	`
	var files, bytes int64
	if err := s.db.QueryRowContext(ctx, q, userID.String()).Scan(&files, &bytes); err != nil {
		return fmt.Errorf("check upload quota: %w", err)
	}
	if files >= galleryDailyMaxFiles {
		return fmt.Errorf("daily upload limit reached (%d files)", galleryDailyMaxFiles)
	}
	if bytes+extraBytes > galleryDailyMaxBytes {
		return fmt.Errorf("daily upload size limit reached")
	}
	return nil
}

func (s *ImageService) hashAndSniffObject(ctx context.Context, storagePath string) (fingerprint, sniffed string, err error) {
	body, contentType, err := s.s3.GetObject(ctx, storagePath)
	if err != nil {
		return "", "", err
	}
	defer body.Close()

	hasher := sha256.New()
	head := make([]byte, 512)
	n, readErr := io.ReadFull(body, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return "", "", readErr
	}
	head = head[:n]
	sniffed = http.DetectContentType(head)
	if _, err := hasher.Write(head); err != nil {
		return "", "", err
	}
	if _, err := io.Copy(hasher, body); err != nil {
		return "", "", err
	}
	_ = contentType
	return hex.EncodeToString(hasher.Sum(nil)), sniffed, nil
}

func galleryMediaKind(contentType string) (string, bool) {
	if kind, ok := galleryImageMIME[contentType]; ok {
		return kind, true
	}
	if kind, ok := galleryVideoMIME[contentType]; ok {
		return kind, true
	}
	return "", false
}

func galleryMIMECompatible(declared, sniffed, kind string) bool {
	declared = strings.ToLower(declared)
	sniffed = strings.ToLower(sniffed)
	if kind == model.MediaKindImage {
		if strings.HasPrefix(sniffed, "image/") {
			return true
		}
		// webp / heic / heif often sniff as application/octet-stream
		switch declared {
		case "image/webp", "image/heic", "image/heif":
			return sniffed == "application/octet-stream"
		default:
			return false
		}
	}
	if kind == model.MediaKindVideo {
		if strings.HasPrefix(sniffed, "video/") {
			return true
		}
		// mp4/quicktime often sniffed as application/octet-stream
		return sniffed == "application/octet-stream"
	}
	return false
}

func galleryFilenameMatchesMIME(filename, contentType string) bool {
	exts, ok := galleryExtByMIME[contentType]
	if !ok {
		return false
	}
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowed := range exts {
		if ext == allowed {
			return true
		}
	}
	return false
}

func sanitizeGalleryFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, "..", ".")
	base = safeFilenameRe.ReplaceAllString(base, "_")
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		base = "upload.bin"
	}
	if len(base) > galleryMaxFilenameLen {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		keep := galleryMaxFilenameLen - len(ext)
		if keep < 8 {
			keep = 8
		}
		if len(stem) > keep {
			stem = stem[:keep]
		}
		base = stem + ext
	}
	return base
}

func parsePartETags(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse part etags: %w", err)
	}
	return out, nil
}

func scanUploadSession(row *sql.Row) (*model.UploadSession, error) {
	s := &model.UploadSession{}
	var albumID, imageID sql.NullString
	err := row.Scan(
		&s.ID,
		&s.UserID,
		&s.OriginalFilename,
		&s.ContentType,
		&s.MediaKind,
		&s.TotalSizeBytes,
		&s.ChunkSizeBytes,
		&s.TotalChunks,
		&s.ReceivedChunks,
		&s.StoragePath,
		&s.S3UploadID,
		&s.PartETags,
		&s.Status,
		&albumID,
		&imageID,
		&s.ExpiresAt,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if albumID.Valid {
		s.AlbumID = &albumID.String
	}
	if imageID.Valid {
		s.ImageID = &imageID.String
	}
	return s, nil
}

func strPtr(v string) *string { return &v }

func parseUUIDPtr(v *string) *uuid.UUID {
	if v == nil || *v == "" {
		return nil
	}
	id, err := uuid.Parse(*v)
	if err != nil {
		return nil
	}
	return &id
}

func awsInt32(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}
