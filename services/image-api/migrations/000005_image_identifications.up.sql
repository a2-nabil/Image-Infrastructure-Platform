CREATE TABLE IF NOT EXISTS image_identifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    image_id UUID REFERENCES images(id) ON DELETE SET NULL,
    fingerprint VARCHAR(64) NOT NULL,
    object_name VARCHAR(255) NOT NULL,
    confidence_score DOUBLE PRECISION NOT NULL,
    summary TEXT NOT NULL,
    key_details JSONB NOT NULL DEFAULT '[]'::jsonb,
    suggested_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    model VARCHAR(128) NOT NULL DEFAULT 'gemini-3.1-flash-lite',
    cache_source VARCHAR(32) NOT NULL DEFAULT 'live',
    embedding JSONB,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_identifications_fingerprint ON image_identifications (fingerprint);
CREATE INDEX IF NOT EXISTS idx_identifications_image_id ON image_identifications (image_id);
CREATE INDEX IF NOT EXISTS idx_identifications_user_id ON image_identifications (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_identifications_image_id
    ON image_identifications (image_id)
    WHERE image_id IS NOT NULL;
