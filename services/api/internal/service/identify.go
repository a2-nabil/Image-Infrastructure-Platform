package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"image-infrastructure-platform/services/api/internal/model"
)

const (
	identifyExactCacheTTL   = 7 * 24 * time.Hour
	identifySemanticMinSim  = 0.92
	identifySemanticScanCap = 200
)

// IdentifyService persists and caches Gemini identification results.
type IdentifyService struct {
	db  *sql.DB
	rdb *redis.Client
}

func NewIdentifyService(db *sql.DB, rdb *redis.Client) *IdentifyService {
	return &IdentifyService{db: db, rdb: rdb}
}

type IdentifyPayload struct {
	ObjectName       string    `json:"object_name"`
	ConfidenceScore  float64   `json:"confidence_score"`
	Summary          string    `json:"summary"`
	KeyDetails       []string  `json:"key_details"`
	SuggestedActions []string  `json:"suggested_actions"`
	Model            string    `json:"model,omitempty"`
	Embedding        []float64 `json:"embedding,omitempty"`
}

type IdentifyLookupResult struct {
	Hit    bool                       `json:"hit"`
	Source string                     `json:"source,omitempty"`
	Data   *model.ImageIdentification `json:"data,omitempty"`
}

func exactCacheKey(fingerprint string) string {
	return "image-api:identify:exact:" + fingerprint
}

// Lookup tries Redis exact cache, then DB exact fingerprint, then semantic embedding match.
func (s *IdentifyService) Lookup(ctx context.Context, fingerprint string, embedding []float64) (*IdentifyLookupResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("identify service is not initialized")
	}
	fingerprint = strings.TrimSpace(strings.ToLower(fingerprint))
	if fingerprint == "" {
		return nil, fmt.Errorf("fingerprint is required")
	}

	if cached, err := s.getExactRedis(ctx, fingerprint); err == nil && cached != nil {
		cached.CacheSource = model.IdentifyCacheExact
		return &IdentifyLookupResult{Hit: true, Source: model.IdentifyCacheExact, Data: cached}, nil
	}

	if row, err := s.findLatestByFingerprint(ctx, fingerprint); err != nil {
		return nil, err
	} else if row != nil {
		_ = s.setExactRedis(ctx, row)
		row.CacheSource = model.IdentifyCacheExact
		return &IdentifyLookupResult{Hit: true, Source: model.IdentifyCacheExact, Data: row}, nil
	}

	if len(embedding) > 0 {
		if row, score, err := s.findSemantic(ctx, embedding); err != nil {
			return nil, err
		} else if row != nil {
			row.CacheSource = model.IdentifyCacheSemantic
			return &IdentifyLookupResult{
				Hit:    true,
				Source: fmt.Sprintf("%s:%.4f", model.IdentifyCacheSemantic, score),
				Data:   row,
			}, nil
		}
	}

	return &IdentifyLookupResult{Hit: false}, nil
}

// Store upserts an identification for an image/fingerprint and warms Redis exact cache.
func (s *IdentifyService) Store(
	ctx context.Context,
	fingerprint string,
	imageID *string,
	userID *uuid.UUID,
	payload IdentifyPayload,
	cacheSource string,
) (*model.ImageIdentification, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("identify service is not initialized")
	}
	fingerprint = strings.TrimSpace(strings.ToLower(fingerprint))
	if fingerprint == "" {
		return nil, fmt.Errorf("fingerprint is required")
	}
	if strings.TrimSpace(payload.ObjectName) == "" || strings.TrimSpace(payload.Summary) == "" {
		return nil, fmt.Errorf("object_name and summary are required")
	}
	if payload.KeyDetails == nil {
		payload.KeyDetails = []string{}
	}
	if payload.SuggestedActions == nil {
		payload.SuggestedActions = []string{}
	}
	if payload.Model == "" {
		payload.Model = "gemini-3.1-flash-lite"
	}
	if cacheSource == "" {
		cacheSource = model.IdentifyCacheLive
	}

	detailsJSON, err := json.Marshal(payload.KeyDetails)
	if err != nil {
		return nil, err
	}
	actionsJSON, err := json.Marshal(payload.SuggestedActions)
	if err != nil {
		return nil, err
	}
	var embeddingJSON any
	if len(payload.Embedding) > 0 {
		b, err := json.Marshal(payload.Embedding)
		if err != nil {
			return nil, err
		}
		embeddingJSON = b
	}

	var userIDVal any
	if userID != nil {
		userIDVal = userID.String()
	}
	var imageIDVal any
	if imageID != nil && *imageID != "" {
		imageIDVal = *imageID
	}

	var row *model.ImageIdentification
	if imageIDVal != nil {
		const updateSQL = `
			UPDATE image_identifications SET
				fingerprint = $2,
				object_name = $3,
				confidence_score = $4,
				summary = $5,
				key_details = $6::jsonb,
				suggested_actions = $7::jsonb,
				model = $8,
				cache_source = $9,
				embedding = COALESCE($10::jsonb, embedding),
				user_id = COALESCE($11, user_id),
				updated_at = NOW()
			WHERE image_id = $1
			RETURNING id, image_id, fingerprint, object_name, confidence_score, summary,
			          key_details, suggested_actions, model, cache_source, embedding, user_id,
			          created_at, updated_at
		`
		row, err = s.scanIdentification(s.db.QueryRowContext(
			ctx, updateSQL,
			imageIDVal, fingerprint, payload.ObjectName, payload.ConfidenceScore, payload.Summary,
			detailsJSON, actionsJSON, payload.Model, cacheSource, embeddingJSON, userIDVal,
		))
		if err != nil {
			return nil, fmt.Errorf("store identification: %w", err)
		}
		if row == nil {
			const insertLinkedSQL = `
				INSERT INTO image_identifications (
					image_id, fingerprint, object_name, confidence_score, summary,
					key_details, suggested_actions, model, cache_source, embedding, user_id
				) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11)
				RETURNING id, image_id, fingerprint, object_name, confidence_score, summary,
				          key_details, suggested_actions, model, cache_source, embedding, user_id,
				          created_at, updated_at
			`
			row, err = s.scanIdentification(s.db.QueryRowContext(
				ctx, insertLinkedSQL,
				imageIDVal, fingerprint, payload.ObjectName, payload.ConfidenceScore, payload.Summary,
				detailsJSON, actionsJSON, payload.Model, cacheSource, embeddingJSON, userIDVal,
			))
		}
	} else {
		const insertSQL = `
			INSERT INTO image_identifications (
				image_id, fingerprint, object_name, confidence_score, summary,
				key_details, suggested_actions, model, cache_source, embedding, user_id
			) VALUES (NULL, $1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9::jsonb, $10)
			RETURNING id, image_id, fingerprint, object_name, confidence_score, summary,
			          key_details, suggested_actions, model, cache_source, embedding, user_id,
			          created_at, updated_at
		`
		row, err = s.scanIdentification(s.db.QueryRowContext(
			ctx, insertSQL,
			fingerprint, payload.ObjectName, payload.ConfidenceScore, payload.Summary,
			detailsJSON, actionsJSON, payload.Model, cacheSource, embeddingJSON, userIDVal,
		))
	}
	if err != nil {
		return nil, fmt.Errorf("store identification: %w", err)
	}

	_ = s.setExactRedis(ctx, row)
	return row, nil
}

// GetByImageID returns the identification linked to an image, if any.
func (s *IdentifyService) GetByImageID(ctx context.Context, imageID string) (*model.ImageIdentification, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("identify service is not initialized")
	}
	const q = `
		SELECT id, image_id, fingerprint, object_name, confidence_score, summary,
		       key_details, suggested_actions, model, cache_source, embedding, user_id,
		       created_at, updated_at
		FROM image_identifications
		WHERE image_id = $1
		LIMIT 1
	`
	return s.scanIdentification(s.db.QueryRowContext(ctx, q, imageID))
}

// MapByImageIDs loads identifications for many images.
func (s *IdentifyService) MapByImageIDs(ctx context.Context, imageIDs []string) (map[string]*model.ImageIdentification, error) {
	out := make(map[string]*model.ImageIdentification, len(imageIDs))
	if len(imageIDs) == 0 {
		return out, nil
	}
	for _, id := range imageIDs {
		row, err := s.GetByImageID(ctx, id)
		if err != nil {
			return nil, err
		}
		if row != nil && row.ImageID != nil {
			out[*row.ImageID] = row
		}
	}
	return out, nil
}

// DeleteForImage removes AI identification rows for an image and clears the
// fingerprint Redis cache. Safe to call when no row exists.
func (s *IdentifyService) DeleteForImage(ctx context.Context, imageID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("identify service is not initialized")
	}
	if imageID == "" {
		return nil
	}

	const selectSQL = `
		SELECT fingerprint
		FROM image_identifications
		WHERE image_id = $1::uuid
	`
	rows, err := s.db.QueryContext(ctx, selectSQL, imageID)
	if err != nil {
		return fmt.Errorf("list identifications for delete: %w", err)
	}
	fingerprints := make([]string, 0, 1)
	for rows.Next() {
		var fp string
		if scanErr := rows.Scan(&fp); scanErr != nil {
			rows.Close()
			return scanErr
		}
		if fp != "" {
			fingerprints = append(fingerprints, fp)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	const deleteSQL = `
		DELETE FROM image_identifications
		WHERE image_id = $1::uuid
	`
	if _, err := s.db.ExecContext(ctx, deleteSQL, imageID); err != nil {
		return fmt.Errorf("delete identifications: %w", err)
	}

	if s.rdb != nil {
		for _, fp := range fingerprints {
			_ = s.rdb.Del(ctx, exactCacheKey(fp)).Err()
		}
	}
	return nil
}

func (s *IdentifyService) getExactRedis(ctx context.Context, fingerprint string) (*model.ImageIdentification, error) {
	if s.rdb == nil {
		return nil, redis.Nil
	}
	raw, err := s.rdb.Get(ctx, exactCacheKey(fingerprint)).Bytes()
	if err != nil {
		return nil, err
	}
	var row model.ImageIdentification
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *IdentifyService) setExactRedis(ctx context.Context, row *model.ImageIdentification) error {
	if s.rdb == nil || row == nil {
		return nil
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, exactCacheKey(row.Fingerprint), payload, identifyExactCacheTTL).Err()
}

func (s *IdentifyService) findLatestByFingerprint(ctx context.Context, fingerprint string) (*model.ImageIdentification, error) {
	const q = `
		SELECT id, image_id, fingerprint, object_name, confidence_score, summary,
		       key_details, suggested_actions, model, cache_source, embedding, user_id,
		       created_at, updated_at
		FROM image_identifications
		WHERE fingerprint = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`
	return s.scanIdentification(s.db.QueryRowContext(ctx, q, fingerprint))
}

func (s *IdentifyService) findSemantic(ctx context.Context, embedding []float64) (*model.ImageIdentification, float64, error) {
	const q = `
		SELECT id, image_id, fingerprint, object_name, confidence_score, summary,
		       key_details, suggested_actions, model, cache_source, embedding, user_id,
		       created_at, updated_at
		FROM image_identifications
		WHERE embedding IS NOT NULL
		ORDER BY updated_at DESC
		LIMIT $1
	`
	rows, err := s.db.QueryContext(ctx, q, identifySemanticScanCap)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var best *model.ImageIdentification
	bestScore := 0.0
	for rows.Next() {
		row, err := s.scanIdentificationRows(rows)
		if err != nil {
			return nil, 0, err
		}
		if len(row.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(embedding, row.Embedding)
		if score >= identifySemanticMinSim && score > bestScore {
			bestScore = score
			copied := *row
			best = &copied
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return best, bestScore, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (s *IdentifyService) scanIdentification(row scannable) (*model.ImageIdentification, error) {
	item := &model.ImageIdentification{}
	var imageID, userID sql.NullString
	var detailsRaw, actionsRaw, embeddingRaw []byte
	err := row.Scan(
		&item.ID,
		&imageID,
		&item.Fingerprint,
		&item.ObjectName,
		&item.ConfidenceScore,
		&item.Summary,
		&detailsRaw,
		&actionsRaw,
		&item.Model,
		&item.CacheSource,
		&embeddingRaw,
		&userID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if imageID.Valid {
		item.ImageID = &imageID.String
	}
	if userID.Valid {
		item.UserID = &userID.String
	}
	_ = json.Unmarshal(detailsRaw, &item.KeyDetails)
	_ = json.Unmarshal(actionsRaw, &item.SuggestedActions)
	if len(embeddingRaw) > 0 {
		_ = json.Unmarshal(embeddingRaw, &item.Embedding)
	}
	if item.KeyDetails == nil {
		item.KeyDetails = []string{}
	}
	if item.SuggestedActions == nil {
		item.SuggestedActions = []string{}
	}
	return item, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *IdentifyService) scanIdentificationRows(rows rowScanner) (*model.ImageIdentification, error) {
	return s.scanIdentification(rows)
}

func cosineSimilarity(a, b []float64) float64 {
	n := len(a)
	if n == 0 || n != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
