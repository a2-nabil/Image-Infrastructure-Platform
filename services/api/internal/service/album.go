package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"image-infrastructure-platform/services/api/internal/model"
)

const (
	albumTitleMaxLen = 120
	albumDescMaxLen  = 500
)

// CreateAlbum creates a new album for the user.
func (s *ImageService) CreateAlbum(ctx context.Context, userID uuid.UUID, title, description string) (*model.Album, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		return nil, fmt.Errorf("album title is required")
	}
	if utf8.RuneCountInString(title) > albumTitleMaxLen {
		return nil, fmt.Errorf("album title is too long")
	}
	if utf8.RuneCountInString(description) > albumDescMaxLen {
		return nil, fmt.Errorf("album description is too long")
	}

	const q = `
		INSERT INTO albums (user_id, title, description)
		VALUES ($1::uuid, $2, $3)
		RETURNING id, user_id, title, description, cover_image_id, created_at, updated_at
	`
	album, err := scanAlbum(s.db.QueryRowContext(ctx, q, userID.String(), title, description))
	if err != nil {
		if strings.Contains(err.Error(), "uq_albums_user_title") {
			return nil, fmt.Errorf("an album with that title already exists")
		}
		return nil, err
	}
	album.ItemCount = 0
	return album, nil
}

// ListAlbums returns albums for the user with item counts.
func (s *ImageService) ListAlbums(ctx context.Context, userID uuid.UUID) ([]*model.Album, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("image service is not initialized")
	}
	const q = `
		SELECT a.id, a.user_id, a.title, a.description, a.cover_image_id,
		       a.created_at, a.updated_at,
		       COALESCE((
		         SELECT COUNT(*) FROM album_items ai WHERE ai.album_id = a.id
		       ), 0) AS item_count
		FROM albums a
		WHERE a.user_id = $1::uuid
		ORDER BY a.updated_at DESC, a.created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, q, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*model.Album, 0)
	for rows.Next() {
		album := &model.Album{}
		var cover sql.NullString
		if err := rows.Scan(
			&album.ID,
			&album.UserID,
			&album.Title,
			&album.Description,
			&cover,
			&album.CreatedAt,
			&album.UpdatedAt,
			&album.ItemCount,
		); err != nil {
			return nil, err
		}
		if cover.Valid {
			album.CoverImageID = &cover.String
		}
		out = append(out, album)
	}
	return out, rows.Err()
}

// RenameAlbum updates title/description for an owned album.
func (s *ImageService) RenameAlbum(ctx context.Context, userID, albumID uuid.UUID, title, description string) (*model.Album, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		return nil, fmt.Errorf("album title is required")
	}
	if utf8.RuneCountInString(title) > albumTitleMaxLen {
		return nil, fmt.Errorf("album title is too long")
	}
	if utf8.RuneCountInString(description) > albumDescMaxLen {
		return nil, fmt.Errorf("album description is too long")
	}
	const q = `
		UPDATE albums
		SET title = $3, description = $4, updated_at = NOW()
		WHERE id = $1::uuid AND user_id = $2::uuid
		RETURNING id, user_id, title, description, cover_image_id, created_at, updated_at
	`
	album, err := scanAlbum(s.db.QueryRowContext(ctx, q, albumID.String(), userID.String(), title, description))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("album not found")
	}
	return album, err
}

// DeleteAlbum removes an album (items unlink; media stays in library).
func (s *ImageService) DeleteAlbum(ctx context.Context, userID, albumID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM albums WHERE id = $1::uuid AND user_id = $2::uuid
	`, albumID.String(), userID.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("album not found")
	}
	return nil
}

// AddAlbumItems attaches owned media to an album.
func (s *ImageService) AddAlbumItems(ctx context.Context, userID, albumID uuid.UUID, imageIDs []string) error {
	owned, err := s.userOwnsAlbum(ctx, userID, albumID)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("album not found")
	}
	ids := uniqueNonEmpty(imageIDs)
	if len(ids) == 0 {
		return nil
	}

	const ownedSQL = `
		SELECT id::text
		FROM images
		WHERE user_id = $1::uuid
		  AND id = ANY($2::uuid[])
		  AND trashed_at IS NULL
		  AND COALESCE(library_scope, 'tools') = 'gallery'
	`
	rows, err := s.db.QueryContext(ctx, ownedSQL, userID.String(), pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	ownedIDs := make([]string, 0, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ownedIDs = append(ownedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ownedIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for i, id := range ownedIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO album_items (album_id, image_id, position)
			VALUES ($1::uuid, $2::uuid, $3)
			ON CONFLICT (album_id, image_id) DO NOTHING
		`, albumID.String(), id, i); err != nil {
			return err
		}
	}
	coverID := ownedIDs[0]
	if _, err := tx.ExecContext(ctx, `
		UPDATE albums
		SET updated_at = NOW(),
		    cover_image_id = COALESCE(cover_image_id, $2::uuid)
		WHERE id = $1::uuid
	`, albumID.String(), coverID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveAlbumItems removes media from an album.
func (s *ImageService) RemoveAlbumItems(ctx context.Context, userID, albumID uuid.UUID, imageIDs []string) error {
	owned, err := s.userOwnsAlbum(ctx, userID, albumID)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("album not found")
	}
	ids := uniqueNonEmpty(imageIDs)
	if len(ids) == 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM album_items
		WHERE album_id = $1::uuid
		  AND image_id = ANY($2::uuid[])
	`, albumID.String(), pq.Array(ids))
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE albums SET updated_at = NOW() WHERE id = $1::uuid
	`, albumID.String())
	return nil
}

func (s *ImageService) userOwnsAlbum(ctx context.Context, userID, albumID uuid.UUID) (bool, error) {
	const q = `SELECT 1 FROM albums WHERE id = $1::uuid AND user_id = $2::uuid LIMIT 1`
	var one int
	err := s.db.QueryRowContext(ctx, q, albumID.String(), userID.String()).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func scanAlbum(row *sql.Row) (*model.Album, error) {
	album := &model.Album{}
	var cover sql.NullString
	err := row.Scan(
		&album.ID,
		&album.UserID,
		&album.Title,
		&album.Description,
		&cover,
		&album.CreatedAt,
		&album.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if cover.Valid {
		album.CoverImageID = &cover.String
	}
	return album, nil
}
