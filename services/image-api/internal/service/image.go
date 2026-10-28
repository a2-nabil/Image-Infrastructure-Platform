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
	"time"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/model"
	"image-infrastructure-platform/services/image-api/internal/storage"
)

const (
	maxUploadBytes    = 10 * 1024 * 1024 // 10MB
	guestRetentionTTL = 72 * time.Hour
)

var allowedMIMETypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

// UploadResult is the outcome of ProcessAndUpload including generated variants.
type UploadResult struct {
	Image          *model.Image
	Variants       []*model.ImageVariant
	Deduplicated   bool
	GuestSessionID string
}

// VariantJobPublisher publishes async variant-generation jobs.
type VariantJobPublisher interface {
	EnqueueVariantJob(ctx context.Context, imageID, storagePath string, userID *string) error
}

// ImageService handles upload validation, S3 ingestion, and persistence.
type ImageService struct {
	db    *sql.DB
	s3    *storage.S3Client
	queue VariantJobPublisher
}

// NewImageService constructs an ImageService.
func NewImageService(db *sql.DB, s3Client *storage.S3Client) *ImageService {
	return &ImageService{db: db, s3: s3Client}
}

// SetVariantQueue wires the async job publisher used after raw upload.
func (s *ImageService) SetVariantQueue(q VariantJobPublisher) {
	if s != nil {
		s.queue = q
	}
}

// ProcessAndUpload validates the multipart image, uploads raw bytes to S3,
// persists metadata as processing, and enqueues async variant generation.
func (s *ImageService) ProcessAndUpload(ctx context.Context, fileHeader *multipart.FileHeader, authUserID *uuid.UUID, guestSessionID string, clientLat, clientLng *float64) (*UploadResult, error) {
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

	userID := userIDString(authUserID)
	if userID == nil {
		if guestSessionID == "" {
			guestSessionID, err = newUUID()
			if err != nil {
				return nil, fmt.Errorf("generate guest session id: %w", err)
			}
		}
	} else {
		guestSessionID = ""
	}

	existing, err := s.findReadyByFingerprint(ctx, fingerprint, userID, guestSessionID)
	if err != nil {
		return nil, fmt.Errorf("lookup fingerprint: %w", err)
	}
	if existing != nil {
		variants, err := s.listVariants(ctx, existing.ID)
		if err != nil {
			return nil, fmt.Errorf("load existing variants: %w", err)
		}
		return &UploadResult{
			Image:          existing,
			Variants:       variants,
			Deduplicated:   true,
			GuestSessionID: guestSessionID,
		}, nil
	}

	objectID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("generate object id: %w", err)
	}

	filename := sanitizeFilename(fileHeader.Filename)
	storagePath := rawStoragePath(userID, objectID, filename)

	if _, err := s.s3.UploadImage(ctx, storagePath, bytes.NewReader(payload), mimeType); err != nil {
		return nil, fmt.Errorf("upload to s3: %w", err)
	}

	width, height := decodeDimensions(payload)
	location := ResolveLocation(payload, clientLat, clientLng)

	img := &model.Image{
		Fingerprint:      fingerprint,
		OriginalFilename: filename,
		StoragePath:      storagePath,
		MimeType:         mimeType,
		FileSizeBytes:    int64(len(payload)),
		Width:            width,
		Height:           height,
		Status:           model.ImageStatusProcessing,
		UserID:           userID,
		Latitude:         location.Latitude,
		Longitude:        location.Longitude,
		Altitude:         location.Altitude,
	}
	if location.Source != "" {
		src := location.Source
		img.LocationSource = &src
	}
	if userID == nil {
		guestCopy := guestSessionID
		img.GuestSessionID = &guestCopy
		expires := time.Now().UTC().Add(guestRetentionTTL)
		img.ExpiresAt = &expires
	}

	const insertSQL = `
		INSERT INTO images (
			fingerprint, original_filename, storage_path, mime_type,
			file_size_bytes, width, height, status, user_id, guest_session_id, expires_at,
			latitude, longitude, altitude, location_source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
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
		nullableString(img.UserID),
		nullableString(img.GuestSessionID),
		nullableTime(img.ExpiresAt),
		nullableFloat(img.Latitude),
		nullableFloat(img.Longitude),
		nullableFloat(img.Altitude),
		nullableString(img.LocationSource),
	).Scan(&img.ID, &img.CreatedAt, &img.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("persist image metadata: %w", err)
	}

	// Event-driven path: enqueue variant work and return immediately.
	if s.queue != nil {
		if err := s.queue.EnqueueVariantJob(ctx, img.ID, storagePath, userID); err != nil {
			_ = s.updateImageStatus(ctx, img.ID, model.ImageStatusFailed)
			return nil, fmt.Errorf("enqueue variant job: %w", err)
		}
		return &UploadResult{
			Image:          img,
			Variants:       nil,
			Deduplicated:   false,
			GuestSessionID: guestSessionID,
		}, nil
	}

	// Fallback synchronous processing (tests / no queue wired).
	variants, err := s.GenerateVariants(ctx, img.ID, userID, bytes.NewReader(payload))
	if err != nil {
		_ = s.updateImageStatus(ctx, img.ID, model.ImageStatusFailed)
		return nil, fmt.Errorf("generate variants: %w", err)
	}
	if err := s.persistVariants(ctx, variants); err != nil {
		_ = s.updateImageStatus(ctx, img.ID, model.ImageStatusFailed)
		return nil, fmt.Errorf("persist variants: %w", err)
	}
	if err := s.updateImageStatus(ctx, img.ID, model.ImageStatusReady); err != nil {
		return nil, fmt.Errorf("mark image ready: %w", err)
	}
	img.Status = model.ImageStatusReady

	return &UploadResult{
		Image:          img,
		Variants:       variants,
		Deduplicated:   false,
		GuestSessionID: guestSessionID,
	}, nil
}

// ClaimGuestMedia promotes ephemeral guest images to permanent user assets.
func (s *ImageService) ClaimGuestMedia(ctx context.Context, userID uuid.UUID, guestSessionID string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("image service is not initialized")
	}
	if guestSessionID == "" {
		return 0, fmt.Errorf("guest_session_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const updateSQL = `
		UPDATE images
		SET user_id = $1, expires_at = NULL, guest_session_id = NULL, updated_at = NOW()
		WHERE guest_session_id = $2 AND expires_at > NOW()
	`
	res, err := tx.ExecContext(ctx, updateSQL, userID.String(), guestSessionID)
	if err != nil {
		return 0, fmt.Errorf("claim guest media: %w", err)
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read claimed count: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit claim transaction: %w", err)
	}
	return int(claimed), nil
}

func (s *ImageService) findReadyByFingerprint(ctx context.Context, fingerprint string, ownerUserID *string, guestSessionID string) (*model.Image, error) {
	// Tenant-scoped dedupe: authenticated users only hit their own ready rows;
	// guests only hit rows for the same guest session.
	const query = `
		SELECT id, fingerprint, original_filename, storage_path, mime_type,
		       file_size_bytes, width, height, status, user_id, guest_session_id,
		       expires_at, latitude, longitude, altitude, location_source,
		       created_at, updated_at
		FROM images
		WHERE fingerprint = $1
		  AND status = 'ready'
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND (
		        ($2::text IS NOT NULL AND user_id = $2)
		     OR ($2::text IS NULL AND guest_session_id = $3 AND user_id IS NULL)
		  )
		LIMIT 1
	`

	img := &model.Image{}
	var userID, guestSessionIDCol, locationSource sql.NullString
	var expiresAt sql.NullTime
	var latitude, longitude, altitude sql.NullFloat64
	err := s.db.QueryRowContext(ctx, query, fingerprint, nullableString(ownerUserID), guestSessionID).Scan(
		&img.ID,
		&img.Fingerprint,
		&img.OriginalFilename,
		&img.StoragePath,
		&img.MimeType,
		&img.FileSizeBytes,
		&img.Width,
		&img.Height,
		&img.Status,
		&userID,
		&guestSessionIDCol,
		&expiresAt,
		&latitude,
		&longitude,
		&altitude,
		&locationSource,
		&img.CreatedAt,
		&img.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		img.UserID = &userID.String
	}
	if guestSessionIDCol.Valid {
		img.GuestSessionID = &guestSessionIDCol.String
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		img.ExpiresAt = &t
	}
	if latitude.Valid {
		v := latitude.Float64
		img.Latitude = &v
	}
	if longitude.Valid {
		v := longitude.Float64
		img.Longitude = &v
	}
	if altitude.Valid {
		v := altitude.Float64
		img.Altitude = &v
	}
	if locationSource.Valid {
		img.LocationSource = &locationSource.String
	}
	return img, nil
}

func (s *ImageService) listVariants(ctx context.Context, imageID string) ([]*model.ImageVariant, error) {
	const query = `
		SELECT id, image_id, preset_name, storage_path, mime_type,
		       file_size_bytes, width, height, created_at
		FROM image_variants
		WHERE image_id = $1
		ORDER BY preset_name
	`

	rows, err := s.db.QueryContext(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	variants := make([]*model.ImageVariant, 0)
	for rows.Next() {
		variant := &model.ImageVariant{}
		if err := rows.Scan(
			&variant.ID,
			&variant.ImageID,
			&variant.PresetName,
			&variant.StoragePath,
			&variant.MimeType,
			&variant.FileSizeBytes,
			&variant.Width,
			&variant.Height,
			&variant.CreatedAt,
		); err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return variants, nil
}

// PersistVariants inserts generated variant rows.
func (s *ImageService) PersistVariants(ctx context.Context, variants []*model.ImageVariant) error {
	return s.persistVariants(ctx, variants)
}

// MarkReady sets image status to ready after async processing.
func (s *ImageService) MarkReady(ctx context.Context, imageID string) error {
	return s.updateImageStatus(ctx, imageID, model.ImageStatusReady)
}

// MarkFailed sets image status to failed after async processing errors.
func (s *ImageService) MarkFailed(ctx context.Context, imageID string) error {
	return s.updateImageStatus(ctx, imageID, model.ImageStatusFailed)
}

// OpenRawObject streams the original uploaded object from S3.
func (s *ImageService) OpenRawObject(ctx context.Context, storagePath string) (io.ReadCloser, string, error) {
	if s == nil || s.s3 == nil {
		return nil, "", fmt.Errorf("image service is not initialized")
	}
	if storagePath == "" {
		return nil, "", fmt.Errorf("storage path is required")
	}
	return s.s3.GetObject(ctx, storagePath)
}

// GetImageWithVariants loads an image and its variants (for status polling).
func (s *ImageService) GetImageWithVariants(ctx context.Context, imageID string) (*model.Image, []*model.ImageVariant, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("image service is not initialized")
	}
	img, err := s.findByID(ctx, imageID)
	if err != nil {
		return nil, nil, err
	}
	if img == nil {
		return nil, nil, nil
	}
	variants, err := s.listVariants(ctx, imageID)
	if err != nil {
		return nil, nil, err
	}
	return img, variants, nil
}

func (s *ImageService) findByID(ctx context.Context, imageID string) (*model.Image, error) {
	const query = `
		SELECT id, fingerprint, original_filename, storage_path, mime_type,
		       file_size_bytes, width, height, status, user_id, guest_session_id,
		       expires_at, latitude, longitude, altitude, location_source,
		       created_at, updated_at
		FROM images
		WHERE id = $1
		  AND (expires_at IS NULL OR expires_at > NOW())
		LIMIT 1
	`

	img := &model.Image{}
	var userID, guestSessionID, locationSource sql.NullString
	var expiresAt sql.NullTime
	var latitude, longitude, altitude sql.NullFloat64
	err := s.db.QueryRowContext(ctx, query, imageID).Scan(
		&img.ID,
		&img.Fingerprint,
		&img.OriginalFilename,
		&img.StoragePath,
		&img.MimeType,
		&img.FileSizeBytes,
		&img.Width,
		&img.Height,
		&img.Status,
		&userID,
		&guestSessionID,
		&expiresAt,
		&latitude,
		&longitude,
		&altitude,
		&locationSource,
		&img.CreatedAt,
		&img.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		img.UserID = &userID.String
	}
	if guestSessionID.Valid {
		img.GuestSessionID = &guestSessionID.String
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		img.ExpiresAt = &t
	}
	if latitude.Valid {
		v := latitude.Float64
		img.Latitude = &v
	}
	if longitude.Valid {
		v := longitude.Float64
		img.Longitude = &v
	}
	if altitude.Valid {
		v := altitude.Float64
		img.Altitude = &v
	}
	if locationSource.Valid {
		img.LocationSource = &locationSource.String
	}
	return img, nil
}

func (s *ImageService) persistVariants(ctx context.Context, variants []*model.ImageVariant) error {
	const insertSQL = `
		INSERT INTO image_variants (
			image_id, preset_name, storage_path, mime_type,
			file_size_bytes, width, height
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`

	for _, variant := range variants {
		if err := s.db.QueryRowContext(
			ctx,
			insertSQL,
			variant.ImageID,
			variant.PresetName,
			variant.StoragePath,
			variant.MimeType,
			variant.FileSizeBytes,
			variant.Width,
			variant.Height,
		).Scan(&variant.ID, &variant.CreatedAt); err != nil {
			return fmt.Errorf("insert variant %q: %w", variant.PresetName, err)
		}
	}
	return nil
}

func (s *ImageService) updateImageStatus(ctx context.Context, imageID, status string) error {
	const updateSQL = `
		UPDATE images
		SET status = $2, updated_at = NOW()
		WHERE id = $1
	`
	_, err := s.db.ExecContext(ctx, updateSQL, imageID, status)
	return err
}

// ObjectURL builds the S3 object URL for a stored image.
func (s *ImageService) ObjectURL(img *model.Image) string {
	if img == nil || s == nil || s.s3 == nil {
		return ""
	}
	return s.s3.ObjectURL(img.StoragePath)
}

// FindImageByID loads a non-expired image by id.
func (s *ImageService) FindImageByID(ctx context.Context, imageID string) (*model.Image, error) {
	return s.findByID(ctx, imageID)
}

// PresignObjectURL returns a time-limited GET URL for an object key.
func (s *ImageService) PresignObjectURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s == nil || s.s3 == nil {
		return "", fmt.Errorf("image service is not initialized")
	}
	return s.s3.PresignGetURL(ctx, key, ttl)
}

// VariantProxyPath returns the API path that streams a variant without public S3 access.
func (s *ImageService) VariantProxyPath(imageID, presetName string) string {
	return fmt.Sprintf("/api/v1/images/%s/variants/%s", imageID, presetName)
}

// FindVariantByPreset loads a variant row by image id and preset name.
// Expired guest images are treated as not found.
func (s *ImageService) FindVariantByPreset(ctx context.Context, imageID, presetName string) (*model.ImageVariant, error) {
	const query = `
		SELECT v.id, v.image_id, v.preset_name, v.storage_path, v.mime_type,
		       v.file_size_bytes, v.width, v.height, v.created_at
		FROM image_variants v
		INNER JOIN images i ON i.id = v.image_id
		WHERE v.image_id = $1
		  AND v.preset_name = $2
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		LIMIT 1
	`

	variant := &model.ImageVariant{}
	err := s.db.QueryRowContext(ctx, query, imageID, presetName).Scan(
		&variant.ID,
		&variant.ImageID,
		&variant.PresetName,
		&variant.StoragePath,
		&variant.MimeType,
		&variant.FileSizeBytes,
		&variant.Width,
		&variant.Height,
		&variant.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return variant, nil
}

// OpenVariantObject streams a variant object from S3.
func (s *ImageService) OpenVariantObject(ctx context.Context, variant *model.ImageVariant) (io.ReadCloser, string, error) {
	if s == nil || s.s3 == nil {
		return nil, "", fmt.Errorf("image service is not initialized")
	}
	if variant == nil || variant.StoragePath == "" {
		return nil, "", fmt.Errorf("variant storage path is required")
	}

	body, contentType, err := s.s3.GetObject(ctx, variant.StoragePath)
	if err != nil {
		return nil, "", err
	}
	if contentType == "" || contentType == "application/octet-stream" {
		if variant.MimeType != "" {
			contentType = variant.MimeType
		}
	}
	return body, contentType, nil
}

func userIDString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
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
