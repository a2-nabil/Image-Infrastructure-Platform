package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"image-infrastructure-platform/services/image-api/internal/model"
)

// UserService manages user persistence for authentication flows.
type UserService struct {
	db *sql.DB
}

// NewUserService constructs a UserService.
func NewUserService(db *sql.DB) *UserService {
	return &UserService{db: db}
}

// UpsertGoogleUser inserts or updates a user keyed by Google subject id.
func (s *UserService) UpsertGoogleUser(ctx context.Context, googleID, email, name, avatarURL string) (*model.User, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("user service is not initialized")
	}
	if googleID == "" || email == "" {
		return nil, fmt.Errorf("google_id and email are required")
	}

	const upsertSQL = `
		INSERT INTO users (google_id, email, name, avatar_url)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''))
		ON CONFLICT (google_id) DO UPDATE SET
			email = EXCLUDED.email,
			name = COALESCE(EXCLUDED.name, users.name),
			avatar_url = COALESCE(EXCLUDED.avatar_url, users.avatar_url)
		RETURNING id, google_id, email, name, avatar_url, created_at
	`

	user := &model.User{}
	var nameNS, avatarNS sql.NullString
	err := s.db.QueryRowContext(ctx, upsertSQL, googleID, email, name, avatarURL).Scan(
		&user.ID,
		&user.GoogleID,
		&user.Email,
		&nameNS,
		&avatarNS,
		&user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert google user: %w", err)
	}
	if nameNS.Valid {
		user.Name = &nameNS.String
	}
	if avatarNS.Valid {
		user.AvatarURL = &avatarNS.String
	}
	return user, nil
}

// EnsureUserByID ensures a user row exists for local/dev token issuance.
func (s *UserService) EnsureUserByID(ctx context.Context, userID uuid.UUID, email string) (*model.User, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("user service is not initialized")
	}
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	const selectSQL = `
		SELECT id, google_id, email, name, avatar_url, created_at
		FROM users
		WHERE id = $1
	`
	user := &model.User{}
	var nameNS, avatarNS sql.NullString
	err := s.db.QueryRowContext(ctx, selectSQL, userID.String()).Scan(
		&user.ID,
		&user.GoogleID,
		&user.Email,
		&nameNS,
		&avatarNS,
		&user.CreatedAt,
	)
	if err == nil {
		if nameNS.Valid {
			user.Name = &nameNS.String
		}
		if avatarNS.Valid {
			user.AvatarURL = &avatarNS.String
		}
		return user, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	const insertSQL = `
		INSERT INTO users (id, google_id, email)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id, google_id, email, name, avatar_url, created_at
	`
	devGoogleID := "dev:" + userID.String()
	err = s.db.QueryRowContext(ctx, insertSQL, userID.String(), devGoogleID, email).Scan(
		&user.ID,
		&user.GoogleID,
		&user.Email,
		&nameNS,
		&avatarNS,
		&user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	if nameNS.Valid {
		user.Name = &nameNS.String
	}
	if avatarNS.Valid {
		user.AvatarURL = &avatarNS.String
	}
	return user, nil
}
