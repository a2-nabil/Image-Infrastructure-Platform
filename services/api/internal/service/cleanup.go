package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const defaultTrashRetention = 30 * 24 * time.Hour

var (
	// ErrOwnedImageNotFound is returned when the user has no such image.
	ErrOwnedImageNotFound = errors.New("image not found")
	// ErrOwnedVariantNotFound is returned when the preset is missing for that image.
	ErrOwnedVariantNotFound = errors.New("variant not found")
)

const defaultGuestExpiryBatchSize = 50

// ExpiredGuestImage is a guest image past its retention window.
type ExpiredGuestImage struct {
	ID          string
	StoragePath string
}

// ListExpiredGuestImages returns guest-owned images whose expires_at has passed.
func (s *ImageService) ListExpiredGuestImages(ctx context.Context, limit int) ([]ExpiredGuestImage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if limit <= 0 {
		limit = defaultGuestExpiryBatchSize
	}

	const query = `
		SELECT id, storage_path
		FROM images
		WHERE expires_at IS NOT NULL
		  AND expires_at <= NOW()
		  AND user_id IS NULL
		ORDER BY expires_at ASC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired guest images: %w", err)
	}
	defer rows.Close()

	out := make([]ExpiredGuestImage, 0, limit)
	for rows.Next() {
		var item ExpiredGuestImage
		if err := rows.Scan(&item.ID, &item.StoragePath); err != nil {
			return nil, fmt.Errorf("scan expired guest image: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PurgeExpiredGuestImage deletes S3 originals + variants, then removes DB rows.
// image_variants cascade when the parent images row is deleted.
func (s *ImageService) PurgeExpiredGuestImage(ctx context.Context, imageID string) error {
	if s == nil || s.db == nil || s.s3 == nil {
		return fmt.Errorf("image service is not initialized")
	}
	if imageID == "" {
		return fmt.Errorf("image id is required")
	}

	storagePath, err := s.findExpiredGuestStoragePath(ctx, imageID)
	if err != nil {
		return err
	}
	if storagePath == "" {
		return nil
	}

	variants, err := s.listVariants(ctx, imageID)
	if err != nil {
		return fmt.Errorf("list variants for purge: %w", err)
	}

	keys := make([]string, 0, 1+len(variants))
	keys = append(keys, storagePath)
	for _, variant := range variants {
		if variant != nil && variant.StoragePath != "" {
			keys = append(keys, variant.StoragePath)
		}
	}

	for _, key := range keys {
		if err := s.s3.DeleteObject(ctx, key); err != nil {
			return fmt.Errorf("delete s3 object %q: %w", key, err)
		}
	}

	s.cleanupIdentification(ctx, imageID)

	const deleteSQL = `
		DELETE FROM images
		WHERE id = $1
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW()
		  AND user_id IS NULL
	`
	res, err := s.db.ExecContext(ctx, deleteSQL, imageID)
	if err != nil {
		return fmt.Errorf("delete expired guest image row: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delete rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("expired guest image %s was not deleted (claimed or not expired)", imageID)
	}
	return nil
}

// PurgeExpiredGuestImages sweeps up to limit expired guest images.
// Returns how many were purged successfully.
func (s *ImageService) PurgeExpiredGuestImages(ctx context.Context, limit int, logger *slog.Logger) (int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	items, err := s.ListExpiredGuestImages(ctx, limit)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, item := range items {
		if err := s.PurgeExpiredGuestImage(ctx, item.ID); err != nil {
			logger.Error("guest expiry purge failed",
				"image_id", item.ID,
				"error", err,
			)
			continue
		}
		purged++
		logger.Info("guest expiry purged",
			"image_id", item.ID,
			"storage_path", item.StoragePath,
		)
	}
	return purged, nil
}

func (s *ImageService) findExpiredGuestStoragePath(ctx context.Context, imageID string) (string, error) {
	const query = `
		SELECT storage_path
		FROM images
		WHERE id = $1
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW()
		  AND user_id IS NULL
		LIMIT 1
	`
	var storagePath string
	err := s.db.QueryRowContext(ctx, query, imageID).Scan(&storagePath)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load expired guest image: %w", err)
	}
	return storagePath, nil
}

// TrashOwnedImages soft-deletes owned images (Google Photos "Move to trash").
// Variants stay attached to the parent image: they leave the library with it
// and return together on restore. Permanent purge removes original + all variants.
func (s *ImageService) TrashOwnedImages(ctx context.Context, userID uuid.UUID, imageIDs []string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("image service is not initialized")
	}
	ids := uniqueNonEmpty(imageIDs)
	if len(ids) == 0 {
		return 0, nil
	}
	const q = `
		UPDATE images
		SET trashed_at = NOW(), updated_at = NOW()
		WHERE user_id = $1::uuid
		  AND id = ANY($2::uuid[])
		  AND trashed_at IS NULL
	`
	res, err := s.db.ExecContext(ctx, q, userID, pq.Array(ids))
	if err != nil {
		return 0, fmt.Errorf("trash owned images: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RestoreOwnedImages restores images from trash.
func (s *ImageService) RestoreOwnedImages(ctx context.Context, userID uuid.UUID, imageIDs []string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("image service is not initialized")
	}
	ids := uniqueNonEmpty(imageIDs)
	if len(ids) == 0 {
		return 0, nil
	}
	const q = `
		UPDATE images
		SET trashed_at = NULL, updated_at = NOW()
		WHERE user_id = $1::uuid
		  AND id = ANY($2::uuid[])
		  AND trashed_at IS NOT NULL
	`
	res, err := s.db.ExecContext(ctx, q, userID, pq.Array(ids))
	if err != nil {
		return 0, fmt.Errorf("restore owned images: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PurgeOwnedImages permanently deletes owned images (DB + S3).
func (s *ImageService) PurgeOwnedImages(ctx context.Context, userID uuid.UUID, imageIDs []string) (int, error) {
	ids := uniqueNonEmpty(imageIDs)
	purged := 0
	for _, id := range ids {
		if err := s.DeleteOwnedImage(ctx, userID, id); err != nil {
			if errors.Is(err, ErrOwnedImageNotFound) {
				continue
			}
			return purged, err
		}
		purged++
	}
	return purged, nil
}

// ListExpiredTrashedImages returns trashed images past retention for background purge.
func (s *ImageService) ListExpiredTrashedImages(ctx context.Context, olderThan time.Duration, limit int) ([]ExpiredGuestImage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	if olderThan <= 0 {
		olderThan = defaultTrashRetention
	}
	if limit <= 0 {
		limit = defaultGuestExpiryBatchSize
	}
	cutoff := time.Now().UTC().Add(-olderThan)
	const q = `
		SELECT id, storage_path
		FROM images
		WHERE trashed_at IS NOT NULL
		  AND trashed_at <= $1
		ORDER BY trashed_at ASC
		LIMIT $2
	`
	rows, err := s.db.QueryContext(ctx, q, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired trash: %w", err)
	}
	defer rows.Close()
	out := make([]ExpiredGuestImage, 0, limit)
	for rows.Next() {
		var item ExpiredGuestImage
		if err := rows.Scan(&item.ID, &item.StoragePath); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// PurgeTrashedImageByID permanently deletes a trashed image (used by sweeper; no user check).
func (s *ImageService) PurgeTrashedImageByID(ctx context.Context, imageID string) error {
	if s == nil || s.db == nil || s.s3 == nil {
		return fmt.Errorf("image service is not initialized")
	}
	const loadSQL = `
		SELECT storage_path, user_id
		FROM images
		WHERE id = $1::uuid
		  AND trashed_at IS NOT NULL
		LIMIT 1
	`
	var storagePath string
	var userID sql.NullString
	err := s.db.QueryRowContext(ctx, loadSQL, imageID).Scan(&storagePath, &userID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if userID.Valid {
		uid, parseErr := uuid.Parse(userID.String)
		if parseErr != nil {
			return parseErr
		}
		return s.DeleteOwnedImage(ctx, uid, imageID)
	}
	// Guest trashed (shouldn't normally happen) — purge like guest expiry.
	variants, err := s.listVariants(ctx, imageID)
	if err != nil {
		return err
	}
	keys := []string{}
	if storagePath != "" {
		keys = append(keys, storagePath)
	}
	for _, v := range variants {
		if v != nil && v.StoragePath != "" {
			keys = append(keys, v.StoragePath)
		}
	}
	for _, key := range keys {
		if err := s.s3.DeleteObject(ctx, key); err != nil {
			return err
		}
	}
	s.cleanupIdentification(ctx, imageID)
	_, err = s.db.ExecContext(ctx, `DELETE FROM images WHERE id = $1::uuid`, imageID)
	return err
}

// PurgeExpiredTrash sweeps trashed images older than retention.
func (s *ImageService) PurgeExpiredTrash(ctx context.Context, olderThan time.Duration, limit int, logger *slog.Logger) (int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	items, err := s.ListExpiredTrashedImages(ctx, olderThan, limit)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, item := range items {
		if err := s.PurgeTrashedImageByID(ctx, item.ID); err != nil {
			logger.Error("trash purge failed", "image_id", item.ID, "error", err)
			continue
		}
		purged++
		logger.Info("trash purged", "image_id", item.ID)
	}
	return purged, nil
}

func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// DeleteOwnedImage removes the original and all variants from S3, then deletes
// the images row (variants cascade). Only the owning user may delete.
func (s *ImageService) DeleteOwnedImage(ctx context.Context, userID uuid.UUID, imageID string) error {
	if s == nil || s.db == nil || s.s3 == nil {
		return fmt.Errorf("image service is not initialized")
	}
	if imageID == "" {
		return fmt.Errorf("image id is required")
	}

	const loadSQL = `
		SELECT storage_path
		FROM images
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		LIMIT 1
	`
	var storagePath string
	err := s.db.QueryRowContext(ctx, loadSQL, imageID, userID).Scan(&storagePath)
	if err == sql.ErrNoRows {
		return ErrOwnedImageNotFound
	}
	if err != nil {
		return fmt.Errorf("load owned image: %w", err)
	}

	variants, err := s.listVariants(ctx, imageID)
	if err != nil {
		return fmt.Errorf("list variants for delete: %w", err)
	}

	keys := make([]string, 0, 1+len(variants))
	if storagePath != "" {
		keys = append(keys, storagePath)
	}
	for _, variant := range variants {
		if variant != nil && variant.StoragePath != "" {
			keys = append(keys, variant.StoragePath)
		}
	}

	for _, key := range keys {
		if err := s.s3.DeleteObject(ctx, key); err != nil {
			return fmt.Errorf("delete s3 object %q: %w", key, err)
		}
	}

	s.cleanupIdentification(ctx, imageID)

	const deleteSQL = `
		DELETE FROM images
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
	`
	res, err := s.db.ExecContext(ctx, deleteSQL, imageID, userID)
	if err != nil {
		return fmt.Errorf("delete owned image row: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read delete rows affected: %w", err)
	}
	if affected == 0 {
		return ErrOwnedImageNotFound
	}
	return nil
}

// DeleteOwnedVariant removes one WebP variant from S3 and Postgres for an owned image.
func (s *ImageService) DeleteOwnedVariant(ctx context.Context, userID uuid.UUID, imageID, preset string) error {
	if s == nil || s.db == nil || s.s3 == nil {
		return fmt.Errorf("image service is not initialized")
	}
	if imageID == "" || preset == "" {
		return fmt.Errorf("image id and preset are required")
	}

	const ownershipSQL = `
		SELECT 1
		FROM images
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		LIMIT 1
	`
	var ok int
	err := s.db.QueryRowContext(ctx, ownershipSQL, imageID, userID).Scan(&ok)
	if err == sql.ErrNoRows {
		return ErrOwnedImageNotFound
	}
	if err != nil {
		return fmt.Errorf("verify image ownership: %w", err)
	}

	const variantSQL = `
		SELECT id, storage_path
		FROM image_variants
		WHERE image_id = $1::uuid
		  AND preset_name = $2
		LIMIT 1
	`
	var variantID, storagePath string
	err = s.db.QueryRowContext(ctx, variantSQL, imageID, preset).Scan(&variantID, &storagePath)
	if err == sql.ErrNoRows {
		return ErrOwnedVariantNotFound
	}
	if err != nil {
		return fmt.Errorf("load owned variant: %w", err)
	}

	if storagePath != "" {
		if err := s.s3.DeleteObject(ctx, storagePath); err != nil {
			return fmt.Errorf("delete s3 variant %q: %w", storagePath, err)
		}
	}

	const deleteSQL = `
		DELETE FROM image_variants
		WHERE id = $1::uuid
		  AND image_id = $2::uuid
	`
	res, err := s.db.ExecContext(ctx, deleteSQL, variantID, imageID)
	if err != nil {
		return fmt.Errorf("delete variant row: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read variant delete rows affected: %w", err)
	}
	if affected == 0 {
		return ErrOwnedVariantNotFound
	}
	return nil
}
