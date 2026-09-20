package model

import "time"

const (
	ImageStatusUploaded   = "uploaded"
	ImageStatusProcessing = "processing"
	ImageStatusReady      = "ready"
	ImageStatusFailed     = "failed"
)

// Image maps to the images table.
type Image struct {
	ID               string     `db:"id" json:"id"`
	Fingerprint      string     `db:"fingerprint" json:"fingerprint"`
	OriginalFilename string     `db:"original_filename" json:"original_filename"`
	StoragePath      string     `db:"storage_path" json:"storage_path"`
	MimeType         string     `db:"mime_type" json:"mime_type"`
	FileSizeBytes    int64      `db:"file_size_bytes" json:"file_size_bytes"`
	Width            *int       `db:"width" json:"width,omitempty"`
	Height           *int       `db:"height" json:"height,omitempty"`
	Status           string     `db:"status" json:"status"`
	UserID           *string    `db:"user_id" json:"user_id,omitempty"`
	GuestSessionID   *string    `db:"guest_session_id" json:"guest_session_id,omitempty"`
	ExpiresAt        *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	Latitude         *float64   `db:"latitude" json:"latitude,omitempty"`
	Longitude        *float64   `db:"longitude" json:"longitude,omitempty"`
	Altitude         *float64   `db:"altitude" json:"altitude,omitempty"`
	LocationSource   *string    `db:"location_source" json:"location_source,omitempty"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
}

// ImageVariant maps to the image_variants table.
type ImageVariant struct {
	ID            string    `db:"id" json:"id"`
	ImageID       string    `db:"image_id" json:"image_id"`
	PresetName    string    `db:"preset_name" json:"preset_name"`
	StoragePath   string    `db:"storage_path" json:"storage_path"`
	MimeType      string    `db:"mime_type" json:"mime_type"`
	FileSizeBytes int64     `db:"file_size_bytes" json:"file_size_bytes"`
	Width         int       `db:"width" json:"width"`
	Height        int       `db:"height" json:"height"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}

// User maps to the users table.
type User struct {
	ID        string    `db:"id" json:"id"`
	GoogleID  string    `db:"google_id" json:"google_id"`
	Email     string    `db:"email" json:"email"`
	Name      *string   `db:"name" json:"name,omitempty"`
	AvatarURL *string   `db:"avatar_url" json:"avatar_url,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
