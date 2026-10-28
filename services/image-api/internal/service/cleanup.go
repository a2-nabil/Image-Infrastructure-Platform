package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
