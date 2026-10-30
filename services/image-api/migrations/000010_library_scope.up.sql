-- Separate personal Gallery media from Converter / Identify / Dashboard tools library.
ALTER TABLE images
    ADD COLUMN IF NOT EXISTS library_scope VARCHAR(16) NOT NULL DEFAULT 'tools';

ALTER TABLE images
    DROP CONSTRAINT IF EXISTS chk_images_library_scope;

ALTER TABLE images
    ADD CONSTRAINT chk_images_library_scope
    CHECK (library_scope IN ('tools', 'gallery'));

CREATE INDEX IF NOT EXISTS idx_images_user_library_scope
    ON images (user_id, library_scope, created_at DESC)
    WHERE user_id IS NOT NULL;
