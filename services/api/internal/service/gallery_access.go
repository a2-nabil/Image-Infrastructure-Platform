package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GalleryAccessService controls who may use personal gallery APIs.
type GalleryAccessService struct {
	db              *sql.DB
	superAdminEmail string
}

func NewGalleryAccessService(db *sql.DB, superAdminEmail string) *GalleryAccessService {
	email := strings.ToLower(strings.TrimSpace(superAdminEmail))
	if email == "" {
		email = "a2nabil000@gmail.com"
	}
	return &GalleryAccessService{db: db, superAdminEmail: email}
}

func (s *GalleryAccessService) SuperAdminEmail() string {
	if s == nil {
		return "a2nabil000@gmail.com"
	}
	return s.superAdminEmail
}

func (s *GalleryAccessService) IsSuperAdmin(email string) bool {
	return strings.EqualFold(strings.TrimSpace(email), s.SuperAdminEmail())
}

// CanAccessGallery reports whether the email may use /gallery and gallery APIs.
func (s *GalleryAccessService) CanAccessGallery(ctx context.Context, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false, nil
	}
	if s.IsSuperAdmin(email) {
		return true, nil
	}
	if s == nil || s.db == nil {
		return false, fmt.Errorf("gallery access service is not initialized")
	}
	const q = `SELECT 1 FROM gallery_access_emails WHERE email = $1 LIMIT 1`
	var one int
	err := s.db.QueryRowContext(ctx, q, email).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type GalleryAccessEntry struct {
	Email           string    `json:"email"`
	Note            string    `json:"note"`
	CreatedByUserID *string   `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *GalleryAccessService) List(ctx context.Context) ([]GalleryAccessEntry, error) {
	const q = `
		SELECT email, note, created_by_user_id, created_at
		FROM gallery_access_emails
		ORDER BY created_at DESC, email ASC
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GalleryAccessEntry, 0)
	for rows.Next() {
		var e GalleryAccessEntry
		var createdBy sql.NullString
		if err := rows.Scan(&e.Email, &e.Note, &createdBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			e.CreatedByUserID = &createdBy.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *GalleryAccessService) Allow(ctx context.Context, actorUserID uuid.UUID, email, note string) (*GalleryAccessEntry, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	note = strings.TrimSpace(note)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf("invalid email")
	}
	if s.IsSuperAdmin(email) {
		return nil, fmt.Errorf("super admin is always allowed and does not need an allowlist entry")
	}
	if len(note) > 500 {
		return nil, fmt.Errorf("note is too long")
	}
	const q = `
		INSERT INTO gallery_access_emails (email, note, created_by_user_id)
		VALUES ($1, $2, $3::uuid)
		ON CONFLICT (email) DO UPDATE
		SET note = EXCLUDED.note
		RETURNING email, note, created_by_user_id, created_at
	`
	var e GalleryAccessEntry
	var createdBy sql.NullString
	err := s.db.QueryRowContext(ctx, q, email, note, actorUserID.String()).Scan(
		&e.Email, &e.Note, &createdBy, &e.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if createdBy.Valid {
		e.CreatedByUserID = &createdBy.String
	}
	return &e, nil
}

// UserMayAccessGallery loads the user email then checks allowlist / super admin.
func (s *GalleryAccessService) UserMayAccessGallery(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("gallery access service is not initialized")
	}
	const q = `SELECT email FROM users WHERE id = $1::uuid LIMIT 1`
	var email string
	err := s.db.QueryRowContext(ctx, q, userID.String()).Scan(&email)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.CanAccessGallery(ctx, email)
}

func (s *GalleryAccessService) Revoke(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if s.IsSuperAdmin(email) {
		return fmt.Errorf("cannot revoke super admin")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM gallery_access_emails WHERE email = $1`, email)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("email not found on allowlist")
	}
	return nil
}
