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
	EnqueueVariantJob(ctx context.Context, imageID, storagePath string, userID *string, presets []string, quality, customWidth, customHeight int, mediaKind string) error
}

// ConvertOptions controls which WebP variants the worker generates.
type ConvertOptions struct {
	Presets      []string
	Quality      int
	CustomWidth  int
	CustomHeight int
}

// ImageIdentifyCleanup removes AI identification data when an image is purged.
type ImageIdentifyCleanup interface {
	DeleteForImage(ctx context.Context, imageID string) error
}

// ImageService handles upload validation, S3 ingestion, and persistence.
type ImageService struct {
	db       *sql.DB
	s3       *storage.S3Client
	queue    VariantJobPublisher
	identify ImageIdentifyCleanup
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

// SetIdentifyCleanup wires AI row + Redis cleanup on permanent image delete.
func (s *ImageService) SetIdentifyCleanup(c ImageIdentifyCleanup) {
	if s != nil {
		s.identify = c
	}
}

func (s *ImageService) cleanupIdentification(ctx context.Context, imageID string) {
	if s == nil || s.identify == nil || imageID == "" {
		return
	}
	_ = s.identify.DeleteForImage(ctx, imageID)
}

// ProcessAndUpload validates the multipart image, uploads raw bytes to S3,
// persists metadata as processing, and enqueues async variant generation.
// convert controls WebP presets (default: original-size only) and quality.
func (s *ImageService) ProcessAndUpload(ctx context.Context, fileHeader *multipart.FileHeader, authUserID *uuid.UUID, guestSessionID string, clientLat, clientLng *float64, convert ConvertOptions) (*UploadResult, error) {
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

	existing, err := s.findReadyByFingerprint(ctx, fingerprint, userID, guestSessionID, model.LibraryScopeTools)
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

	convert.Presets = NormalizeVariantPresets(convert.Presets)

	// Event-driven path: enqueue variant work and return immediately.
	if s.queue != nil {
		if err := s.queue.EnqueueVariantJob(
			ctx,
			img.ID,
			storagePath,
			userID,
			convert.Presets,
			convert.Quality,
			convert.CustomWidth,
			convert.CustomHeight,
			model.MediaKindImage,
		); err != nil {
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
	variants, err := s.GenerateVariants(
		ctx,
		img.ID,
		userID,
		bytes.NewReader(payload),
		convert.Presets,
		convert.Quality,
		convert.CustomWidth,
		convert.CustomHeight,
	)
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

// ClaimGuestResult summarizes guest → user ownership transfer.
type ClaimGuestResult struct {
	Claimed int // newly owned guest rows
	Merged  int // guest rows folded into an existing user fingerprint
}

type guestClaimRow struct {
	ID          string
	Fingerprint string
	StoragePath string
}

// ClaimGuestMedia promotes ephemeral guest images to permanent user assets.
// If the user already owns the same fingerprint (e.g. uploaded while logged in
// on Converter, then again as guest on Identify), the guest copy is merged and
// removed instead of creating a duplicate gallery item.
func (s *ImageService) ClaimGuestMedia(ctx context.Context, userID uuid.UUID, guestSessionID string) (*ClaimGuestResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if guestSessionID == "" {
		return nil, fmt.Errorf("guest_session_id is required")
	}

	guests, err := s.listClaimableGuestImages(ctx, guestSessionID)
	if err != nil {
		return nil, err
	}

	out := &ClaimGuestResult{}
	uid := userID.String()

	for _, guest := range guests {
		keeperID, err := s.findOwnedImageIDByFingerprint(ctx, uid, guest.Fingerprint)
		if err != nil {
			return out, fmt.Errorf("lookup owned fingerprint: %w", err)
		}
		if keeperID != "" {
			if err := s.mergeGuestImageIntoOwned(ctx, guest, keeperID, uid); err != nil {
				return out, fmt.Errorf("merge guest image %s: %w", guest.ID, err)
			}
			out.Merged++
			continue
		}

		const claimSQL = `
			UPDATE images
			SET user_id = $1::uuid,
			    expires_at = NULL,
			    guest_session_id = NULL,
			    updated_at = NOW()
			WHERE id = $2::uuid
			  AND guest_session_id = $3
			  AND user_id IS NULL
			  AND expires_at > NOW()
		`
		res, err := s.db.ExecContext(ctx, claimSQL, uid, guest.ID, guestSessionID)
		if err != nil {
			return out, fmt.Errorf("claim guest image %s: %w", guest.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return out, err
		}
		if n > 0 {
			out.Claimed++
		}
	}

	// Fold any pre-existing same-fingerprint duplicates under this user
	// (e.g. claim bugs from earlier builds).
	mergedOwned, err := s.mergeDuplicateOwnedFingerprints(ctx, uid)
	if err != nil {
		return out, fmt.Errorf("reconcile owned duplicates: %w", err)
	}
	out.Merged += mergedOwned

	return out, nil
}

func (s *ImageService) listClaimableGuestImages(ctx context.Context, guestSessionID string) ([]guestClaimRow, error) {
	const q = `
		SELECT id, fingerprint, storage_path
		FROM images
		WHERE guest_session_id = $1
		  AND user_id IS NULL
		  AND expires_at IS NOT NULL
		  AND expires_at > NOW()
		ORDER BY created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, q, guestSessionID)
	if err != nil {
		return nil, fmt.Errorf("list claimable guest images: %w", err)
	}
	defer rows.Close()

	out := make([]guestClaimRow, 0)
	for rows.Next() {
		var row guestClaimRow
		if err := rows.Scan(&row.ID, &row.Fingerprint, &row.StoragePath); err != nil {
			return nil, fmt.Errorf("scan claimable guest image: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *ImageService) findOwnedImageIDByFingerprint(ctx context.Context, userID, fingerprint string) (string, error) {
	const q = `
		SELECT id
		FROM images
		WHERE fingerprint = $1
		  AND user_id = $2::uuid
		ORDER BY
		  CASE WHEN trashed_at IS NULL THEN 0 ELSE 1 END,
		  CASE WHEN status = 'ready' THEN 0 ELSE 1 END,
		  created_at ASC
		LIMIT 1
	`
	var id string
	err := s.db.QueryRowContext(ctx, q, fingerprint, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// mergeGuestImageIntoOwned moves missing variants / identify onto the keeper,
// then deletes the guest image (and its S3 objects).
func (s *ImageService) mergeGuestImageIntoOwned(ctx context.Context, guest guestClaimRow, keeperID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin merge tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Move presets the keeper does not already have.
	const moveVariantsSQL = `
		UPDATE image_variants AS guest
		SET image_id = $1::uuid
		WHERE guest.image_id = $2::uuid
		  AND NOT EXISTS (
			SELECT 1
			FROM image_variants keeper
			WHERE keeper.image_id = $1::uuid
			  AND keeper.preset_name = guest.preset_name
		  )
	`
	if _, err := tx.ExecContext(ctx, moveVariantsSQL, keeperID, guest.ID); err != nil {
		return fmt.Errorf("move missing variants: %w", err)
	}

	// Attach guest identify result to keeper when keeper has none.
	const moveIdentifySQL = `
		UPDATE image_identifications AS guest
		SET image_id = $1::uuid,
		    user_id = $3::uuid,
		    updated_at = NOW()
		WHERE guest.image_id = $2::uuid
		  AND NOT EXISTS (
			SELECT 1
			FROM image_identifications keeper
			WHERE keeper.image_id = $1::uuid
		  )
	`
	if _, err := tx.ExecContext(ctx, moveIdentifySQL, keeperID, guest.ID, userID); err != nil {
		return fmt.Errorf("move identify result: %w", err)
	}

	// Collect remaining guest variant keys (not moved) for S3 cleanup.
	remainingVariants, err := s.listVariantsTx(ctx, tx, guest.ID)
	if err != nil {
		return err
	}

	const deleteGuestSQL = `
		DELETE FROM images
		WHERE id = $1::uuid
		  AND user_id IS NULL
	`
	res, err := tx.ExecContext(ctx, deleteGuestSQL, guest.ID)
	if err != nil {
		return fmt.Errorf("delete guest image row: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("guest image %s was not deleted", guest.ID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit merge tx: %w", err)
	}

	keys := make([]string, 0, 1+len(remainingVariants))
	if guest.StoragePath != "" {
		keys = append(keys, guest.StoragePath)
	}
	for _, v := range remainingVariants {
		if v != nil && v.StoragePath != "" {
			keys = append(keys, v.StoragePath)
		}
	}
	if s.s3 != nil {
		for _, key := range keys {
			_ = s.s3.DeleteObject(ctx, key)
		}
	}
	return nil
}

func (s *ImageService) listVariantsTx(ctx context.Context, tx *sql.Tx, imageID string) ([]*model.ImageVariant, error) {
	const q = `
		SELECT id, image_id, preset_name, storage_path, mime_type,
		       file_size_bytes, width, height, created_at
		FROM image_variants
		WHERE image_id = $1
		ORDER BY created_at ASC
	`
	rows, err := tx.QueryContext(ctx, q, imageID)
	if err != nil {
		return nil, fmt.Errorf("list variants in tx: %w", err)
	}
	defer rows.Close()

	out := make([]*model.ImageVariant, 0)
	for rows.Next() {
		v := &model.ImageVariant{}
		if err := rows.Scan(
			&v.ID,
			&v.ImageID,
			&v.PresetName,
			&v.StoragePath,
			&v.MimeType,
			&v.FileSizeBytes,
			&v.Width,
			&v.Height,
			&v.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// mergeDuplicateOwnedFingerprints collapses same-fingerprint rows for one user.
// Keeps the oldest ready/non-trashed row when possible.
func (s *ImageService) mergeDuplicateOwnedFingerprints(ctx context.Context, userID string) (int, error) {
	const dupSQL = `
		SELECT fingerprint, COALESCE(library_scope, 'tools') AS library_scope
		FROM images
		WHERE user_id = $1::uuid
		GROUP BY fingerprint, COALESCE(library_scope, 'tools')
		HAVING COUNT(*) > 1
	`
	rows, err := s.db.QueryContext(ctx, dupSQL, userID)
	if err != nil {
		return 0, fmt.Errorf("list duplicate fingerprints: %w", err)
	}
	defer rows.Close()

	type dupKey struct {
		Fingerprint  string
		LibraryScope string
	}
	dups := make([]dupKey, 0)
	for rows.Next() {
		var key dupKey
		if err := rows.Scan(&key.Fingerprint, &key.LibraryScope); err != nil {
			return 0, err
		}
		dups = append(dups, key)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	merged := 0
	for _, key := range dups {
		fp := key.Fingerprint
		const idsSQL = `
			SELECT id, storage_path
			FROM images
			WHERE user_id = $1::uuid
			  AND fingerprint = $2
			  AND COALESCE(library_scope, 'tools') = $3
			ORDER BY
			  CASE WHEN trashed_at IS NULL THEN 0 ELSE 1 END,
			  CASE WHEN status = 'ready' THEN 0 ELSE 1 END,
			  created_at ASC
		`
		idRows, err := s.db.QueryContext(ctx, idsSQL, userID, fp, key.LibraryScope)
		if err != nil {
			return merged, err
		}
		type ownedRow struct {
			ID          string
			StoragePath string
		}
		owned := make([]ownedRow, 0)
		for idRows.Next() {
			var row ownedRow
			if err := idRows.Scan(&row.ID, &row.StoragePath); err != nil {
				idRows.Close()
				return merged, err
			}
			owned = append(owned, row)
		}
		idRows.Close()
		if err := idRows.Err(); err != nil {
			return merged, err
		}
		if len(owned) < 2 {
			continue
		}
		keeper := owned[0]
		for _, dup := range owned[1:] {
			if err := s.mergeOwnedDuplicateIntoKeeper(ctx, dup.ID, dup.StoragePath, keeper.ID, userID); err != nil {
				return merged, err
			}
			merged++
		}
	}
	return merged, nil
}

func (s *ImageService) mergeOwnedDuplicateIntoKeeper(ctx context.Context, dupID, dupStoragePath, keeperID, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const moveVariantsSQL = `
		UPDATE image_variants AS dup
		SET image_id = $1::uuid
		WHERE dup.image_id = $2::uuid
		  AND NOT EXISTS (
			SELECT 1
			FROM image_variants keeper
			WHERE keeper.image_id = $1::uuid
			  AND keeper.preset_name = dup.preset_name
		  )
	`
	if _, err := tx.ExecContext(ctx, moveVariantsSQL, keeperID, dupID); err != nil {
		return fmt.Errorf("move owned duplicate variants: %w", err)
	}

	const moveIdentifySQL = `
		UPDATE image_identifications AS dup
		SET image_id = $1::uuid,
		    user_id = $3::uuid,
		    updated_at = NOW()
		WHERE dup.image_id = $2::uuid
		  AND NOT EXISTS (
			SELECT 1
			FROM image_identifications keeper
			WHERE keeper.image_id = $1::uuid
		  )
	`
	if _, err := tx.ExecContext(ctx, moveIdentifySQL, keeperID, dupID, userID); err != nil {
		return fmt.Errorf("move owned duplicate identify: %w", err)
	}

	remaining, err := s.listVariantsTx(ctx, tx, dupID)
	if err != nil {
		return err
	}

	const deleteSQL = `
		DELETE FROM images
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
	`
	res, err := tx.ExecContext(ctx, deleteSQL, dupID, userID)
	if err != nil {
		return fmt.Errorf("delete owned duplicate: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("owned duplicate %s was not deleted", dupID)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	keys := make([]string, 0, 1+len(remaining))
	if dupStoragePath != "" {
		keys = append(keys, dupStoragePath)
	}
	for _, v := range remaining {
		if v != nil && v.StoragePath != "" {
			keys = append(keys, v.StoragePath)
		}
	}
	if s.s3 != nil {
		for _, key := range keys {
			_ = s.s3.DeleteObject(ctx, key)
		}
	}
	return nil
}

func (s *ImageService) findReadyByFingerprint(ctx context.Context, fingerprint string, ownerUserID *string, guestSessionID, libraryScope string) (*model.Image, error) {
	// Tenant-scoped dedupe: authenticated users only hit their own ready rows;
	// guests only hit rows for the same guest session. Library scope keeps
	// Gallery media isolated from Converter / Identify / Dashboard.
	if libraryScope != model.LibraryScopeGallery {
		libraryScope = model.LibraryScopeTools
	}
	const queryByUser = `
		SELECT id, fingerprint, original_filename, storage_path, mime_type,
		       file_size_bytes, width, height, status, user_id, guest_session_id,
		       expires_at, latitude, longitude, altitude, location_source,
		       created_at, updated_at
		FROM images
		WHERE fingerprint = $1
		  AND status = 'ready'
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND user_id = $2::uuid
		  AND COALESCE(library_scope, 'tools') = $3
		LIMIT 1
	`
	const queryByGuest = `
		SELECT id, fingerprint, original_filename, storage_path, mime_type,
		       file_size_bytes, width, height, status, user_id, guest_session_id,
		       expires_at, latitude, longitude, altitude, location_source,
		       created_at, updated_at
		FROM images
		WHERE fingerprint = $1
		  AND status = 'ready'
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND user_id IS NULL
		  AND guest_session_id = $2
		  AND COALESCE(library_scope, 'tools') = $3
		LIMIT 1
	`

	img := &model.Image{}
	var userID, guestSessionIDCol, locationSource sql.NullString
	var expiresAt sql.NullTime
	var latitude, longitude, altitude sql.NullFloat64

	var row *sql.Row
	if ownerUserID != nil && *ownerUserID != "" {
		row = s.db.QueryRowContext(ctx, queryByUser, fingerprint, *ownerUserID, libraryScope)
	} else if guestSessionID != "" {
		row = s.db.QueryRowContext(ctx, queryByGuest, fingerprint, guestSessionID, libraryScope)
	} else {
		return nil, nil
	}

	err := row.Scan(
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

// UserImageBundle is an owned image plus its variants for dashboard listing.
type UserImageBundle struct {
	Image    *model.Image
	Variants []*model.ImageVariant
}

// ListImagesForUser returns non-expired images owned by the user (newest first),
// plus the total matching count for pagination.
// When trashOnly is true, only trashed library items are returned.
// ListImagesFilter scopes gallery listing for authenticated users.
type ListImagesFilter struct {
	TrashOnly    bool
	AlbumID      *uuid.UUID
	MediaKind    string // image | video | empty = all
	LibraryScope string // tools | gallery (default tools)
}

func (s *ImageService) ListImagesForUser(ctx context.Context, userID uuid.UUID, limit, offset int, trashOnly bool) ([]UserImageBundle, int, error) {
	return s.ListImagesForUserFiltered(ctx, userID, limit, offset, ListImagesFilter{TrashOnly: trashOnly})
}

func (s *ImageService) ListImagesForUserFiltered(ctx context.Context, userID uuid.UUID, limit, offset int, filter ListImagesFilter) ([]UserImageBundle, int, error) {
	// Heal fingerprint duplicates (guest claim bugs / cross-uploader reuploads).
	if offset == 0 && !filter.TrashOnly && filter.AlbumID == nil {
		if _, err := s.mergeDuplicateOwnedFingerprints(ctx, userID.String()); err != nil {
			return nil, 0, fmt.Errorf("reconcile owned duplicates: %w", err)
		}
	}

	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("image service is not initialized")
	}
	if limit <= 0 || limit > 200 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}

	trashClause := "AND i.trashed_at IS NULL"
	if filter.TrashOnly {
		trashClause = "AND i.trashed_at IS NOT NULL"
	}

	albumJoin := ""
	albumClause := ""
	args := []any{userID.String()}
	argN := 2
	if filter.AlbumID != nil {
		owned, err := s.userOwnsAlbum(ctx, userID, *filter.AlbumID)
		if err != nil {
			return nil, 0, err
		}
		if !owned {
			return nil, 0, fmt.Errorf("album not found")
		}
		albumJoin = "INNER JOIN album_items ai ON ai.image_id = i.id"
		albumClause = fmt.Sprintf("AND ai.album_id = $%d::uuid", argN)
		args = append(args, filter.AlbumID.String())
		argN++
	}

	mediaClause := ""
	if filter.MediaKind == model.MediaKindImage || filter.MediaKind == model.MediaKindVideo {
		mediaClause = fmt.Sprintf("AND i.media_kind = $%d", argN)
		args = append(args, filter.MediaKind)
		argN++
	}

	scope := filter.LibraryScope
	if scope != model.LibraryScopeGallery {
		scope = model.LibraryScopeTools
	}
	scopeClause := fmt.Sprintf("AND COALESCE(i.library_scope, 'tools') = $%d", argN)
	args = append(args, scope)
	argN++

	countQuery := `
		SELECT COUNT(*)
		FROM images i
		` + albumJoin + `
		WHERE i.user_id = $1::uuid
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		  ` + trashClause + `
		  ` + albumClause + `
		  ` + mediaClause + `
		  ` + scopeClause
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count user images: %w", err)
	}

	orderBy := "i.created_at DESC"
	if filter.TrashOnly {
		orderBy = "i.trashed_at DESC NULLS LAST, i.created_at DESC"
	}

	limitPlaceholder := fmt.Sprintf("$%d", argN)
	offsetPlaceholder := fmt.Sprintf("$%d", argN+1)
	queryArgs := append(append([]any{}, args...), limit, offset)

	query := `
		SELECT i.id, i.fingerprint, i.original_filename, i.storage_path, i.mime_type,
		       i.file_size_bytes, i.width, i.height, i.status,
		       COALESCE(i.media_kind, 'image') AS media_kind, i.duration_ms,
		       i.user_id, i.guest_session_id,
		       i.expires_at, i.trashed_at, i.latitude, i.longitude, i.altitude, i.location_source,
		       i.created_at, i.updated_at
		FROM images i
		` + albumJoin + `
		WHERE i.user_id = $1::uuid
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		  ` + trashClause + `
		  ` + albumClause + `
		  ` + mediaClause + `
		  ` + scopeClause + `
		ORDER BY ` + orderBy + `
		LIMIT ` + limitPlaceholder + ` OFFSET ` + offsetPlaceholder

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list user images: %w", err)
	}
	defer rows.Close()

	out := make([]UserImageBundle, 0)
	for rows.Next() {
		img := &model.Image{}
		var ownerID, guestSessionID, locationSource sql.NullString
		var expiresAt, trashedAt sql.NullTime
		var latitude, longitude, altitude sql.NullFloat64
		var durationMs sql.NullInt64
		if err := rows.Scan(
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
			&durationMs,
			&ownerID,
			&guestSessionID,
			&expiresAt,
			&trashedAt,
			&latitude,
			&longitude,
			&altitude,
			&locationSource,
			&img.CreatedAt,
			&img.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user image: %w", err)
		}
		if img.MediaKind == "" {
			img.MediaKind = model.MediaKindImage
		}
		if durationMs.Valid {
			d := int(durationMs.Int64)
			img.DurationMs = &d
		}
		if ownerID.Valid {
			img.UserID = &ownerID.String
		}
		if guestSessionID.Valid {
			img.GuestSessionID = &guestSessionID.String
		}
		if expiresAt.Valid {
			t := expiresAt.Time
			img.ExpiresAt = &t
		}
		if trashedAt.Valid {
			t := trashedAt.Time
			img.TrashedAt = &t
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

		variants, err := s.listVariants(ctx, img.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("list variants for %s: %w", img.ID, err)
		}
		out = append(out, UserImageBundle{Image: img, Variants: variants})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
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

// OpenOriginalObject streams the original uploaded object from S3.
func (s *ImageService) OpenOriginalObject(ctx context.Context, img *model.Image) (io.ReadCloser, string, error) {
	if s == nil || s.s3 == nil {
		return nil, "", fmt.Errorf("image service is not initialized")
	}
	if img == nil || img.StoragePath == "" {
		return nil, "", fmt.Errorf("image storage path is required")
	}

	body, contentType, err := s.s3.GetObject(ctx, img.StoragePath)
	if err != nil {
		return nil, "", err
	}
	if contentType == "" || contentType == "application/octet-stream" {
		if img.MimeType != "" {
			contentType = img.MimeType
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
