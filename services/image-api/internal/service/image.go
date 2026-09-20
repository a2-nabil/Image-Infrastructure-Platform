package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"image-infrastructure-platform/services/image-api/internal/model"
	"image-infrastructure-platform/services/image-api/internal/storage"
)

const maxUploadBytes = 10 * 1024 * 1024 // 10MB

var allowedMIMETypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

// ImageService handles upload validation, S3 ingestion, and persistence.
type ImageService struct {
	db *sql.DB
	s3 *storage.S3Client
}

// NewImageService constructs an ImageService.
func NewImageService(db *sql.DB, s3Client *storage.S3Client) *ImageService {
	return &ImageService{db: db, s3: s3Client}
}

// ProcessAndUpload validates the multipart image, uploads it to S3, and
// persists an images row with status uploaded.
func (s *ImageService) ProcessAndUpload(ctx context.Context, fileHeader *multipart.FileHeader) (*model.Image, error) {
	if s == nil || s.db == nil || s.s3 == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if fileHeader == nil {
		return nil, fmt.Errorf("file is required")
	}
	if fileHeader.Size > maxUploadBytes {
		return nil, fmt.Errorf("file exceeds 10MB limit")
	}

	src, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	defer src.Close()

	limited := io.LimitReader(src, maxUploadBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(payload)) > maxUploadBytes {
		return nil, fmt.Errorf("file exceeds 10MB limit")
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty file")
	}

	sniffLen := 512
	if len(payload) < sniffLen {
		sniffLen = len(payload)
	}
	mimeType := http.DetectContentType(payload[:sniffLen])
	if _, ok := allowedMIMETypes[mimeType]; !ok {
		return nil, fmt.Errorf("unsupported content type %q", mimeType)
	}

	sum := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(sum[:])

	objectID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generate object id: %w", err)
	}

	filename := sanitizeFilename(fileHeader.Filename)
	storagePath := fmt.Sprintf("raw/%s/%s", objectID, filename)

	if _, err := s.s3.UploadImage(ctx, storagePath, bytes.NewReader(payload), mimeType); err != nil {
		return nil, fmt.Errorf("upload to s3: %w", err)
	}

	width, height := decodeDimensions(payload)

	img := &model.Image{
		Fingerprint:      fingerprint,
		OriginalFilename: filename,
		StoragePath:      storagePath,
		MimeType:         mimeType,
		FileSizeBytes:    int64(len(payload)),
		Width:            width,
		Height:           height,
		Status:           model.ImageStatusUploaded,
	}

	const insertSQL = `
		INSERT INTO images (
			fingerprint, original_filename, storage_path, mime_type,
			file_size_bytes, width, height, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`

	err = s.db.QueryRowContext(
		ctx,
		insertSQL,
		img.Fingerprint,
		img.OriginalFilename,
		img.StoragePath,
		img.MimeType,
		img.FileSizeBytes,
		img.Width,
		img.Height,
		img.Status,
	).Scan(&img.ID, &img.CreatedAt, &img.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("persist image metadata: %w", err)
	}

	return img, nil
}

// ObjectURL builds the S3 object URL for a stored image.
func (s *ImageService) ObjectURL(img *model.Image) string {
	if img == nil || s == nil || s.s3 == nil {
		return ""
	}
	return s.s3.ObjectURL(img.StoragePath)
}

func decodeDimensions(payload []byte) (*int, *int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return nil, nil
	}
	w, h := cfg.Width, cfg.Height
	return &w, &h
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		return "upload.bin"
	}
	return base
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
