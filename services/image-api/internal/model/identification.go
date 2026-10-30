package model

import "time"

const (
	IdentifyCacheLive     = "live"
	IdentifyCacheExact    = "exact_cache"
	IdentifyCacheSemantic = "semantic_cache"
)

// ImageIdentification is a persisted Gemini identify result linked to an image/fingerprint.
type ImageIdentification struct {
	ID               string    `json:"id"`
	ImageID          *string   `json:"image_id,omitempty"`
	Fingerprint      string    `json:"fingerprint"`
	ObjectName       string    `json:"object_name"`
	ConfidenceScore  float64   `json:"confidence_score"`
	Summary          string    `json:"summary"`
	KeyDetails       []string  `json:"key_details"`
	SuggestedActions []string  `json:"suggested_actions"`
	Model            string    `json:"model"`
	CacheSource      string    `json:"cache_source"`
	Embedding        []float64 `json:"embedding,omitempty"`
	UserID           *string   `json:"user_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
