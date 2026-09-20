DROP INDEX IF EXISTS idx_images_expires_at;
DROP INDEX IF EXISTS idx_images_guest_session;
DROP INDEX IF EXISTS idx_images_user_id;
ALTER TABLE images
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS guest_session_id,
    DROP COLUMN IF EXISTS user_id;
DROP TABLE IF EXISTS users;
