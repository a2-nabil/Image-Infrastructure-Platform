DROP INDEX IF EXISTS idx_images_user_trashed_at;
ALTER TABLE images DROP COLUMN IF EXISTS trashed_at;
