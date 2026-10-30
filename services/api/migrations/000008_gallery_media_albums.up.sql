-- Personal gallery: media kind, chunked uploads, albums.

ALTER TABLE images
    ADD COLUMN IF NOT EXISTS media_kind VARCHAR(16) NOT NULL DEFAULT 'image',
    ADD COLUMN IF NOT EXISTS duration_ms INT,
    ADD COLUMN IF NOT EXISTS poster_storage_path VARCHAR(512);

CREATE INDEX IF NOT EXISTS idx_images_user_media_kind
    ON images (user_id, media_kind)
    WHERE user_id IS NOT NULL;

-- Resumable / chunked uploads (auth users only).
CREATE TABLE IF NOT EXISTS upload_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    original_filename VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    media_kind VARCHAR(16) NOT NULL,
    total_size_bytes BIGINT NOT NULL,
    chunk_size_bytes INT NOT NULL,
    total_chunks INT NOT NULL,
    received_chunks INT NOT NULL DEFAULT 0,
    storage_path VARCHAR(512) NOT NULL,
    s3_upload_id VARCHAR(512) NOT NULL,
    part_etags JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    album_id UUID,
    image_id UUID REFERENCES images(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_upload_sessions_status CHECK (status IN ('pending', 'assembling', 'completed', 'aborted', 'failed')),
    CONSTRAINT chk_upload_sessions_media_kind CHECK (media_kind IN ('image', 'video')),
    CONSTRAINT chk_upload_sessions_sizes CHECK (
        total_size_bytes > 0
        AND chunk_size_bytes >= 5242880
        AND total_chunks > 0
        AND received_chunks >= 0
        AND received_chunks <= total_chunks
    )
);

CREATE INDEX IF NOT EXISTS idx_upload_sessions_user_status
    ON upload_sessions (user_id, status, expires_at);

-- Albums (user-owned collections).
CREATE TABLE IF NOT EXISTS albums (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(120) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    cover_image_id UUID REFERENCES images(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_albums_user_title UNIQUE (user_id, title)
);

CREATE INDEX IF NOT EXISTS idx_albums_user_updated
    ON albums (user_id, updated_at DESC);

ALTER TABLE upload_sessions
    DROP CONSTRAINT IF EXISTS upload_sessions_album_id_fkey;
ALTER TABLE upload_sessions
    ADD CONSTRAINT upload_sessions_album_id_fkey
    FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS album_items (
    album_id UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    image_id UUID NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (album_id, image_id)
);

CREATE INDEX IF NOT EXISTS idx_album_items_image
    ON album_items (image_id);
CREATE INDEX IF NOT EXISTS idx_album_items_album_position
    ON album_items (album_id, position, added_at);
