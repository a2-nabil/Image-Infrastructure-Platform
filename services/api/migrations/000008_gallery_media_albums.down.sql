DROP TABLE IF EXISTS album_items;
ALTER TABLE upload_sessions DROP CONSTRAINT IF EXISTS upload_sessions_album_id_fkey;
DROP TABLE IF EXISTS albums;
DROP TABLE IF EXISTS upload_sessions;
DROP INDEX IF EXISTS idx_images_user_media_kind;
ALTER TABLE images
    DROP COLUMN IF EXISTS poster_storage_path,
    DROP COLUMN IF EXISTS duration_ms,
    DROP COLUMN IF EXISTS media_kind;
