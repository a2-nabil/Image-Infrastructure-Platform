ALTER TABLE images
    ADD COLUMN IF NOT EXISTS trashed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_images_user_trashed_at
    ON images (user_id, trashed_at)
    WHERE user_id IS NOT NULL;
