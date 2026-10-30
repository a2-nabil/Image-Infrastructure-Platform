DROP INDEX IF EXISTS idx_images_user_library_scope;
ALTER TABLE images DROP CONSTRAINT IF EXISTS chk_images_library_scope;
ALTER TABLE images DROP COLUMN IF EXISTS library_scope;
