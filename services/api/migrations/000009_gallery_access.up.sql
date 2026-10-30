-- Emails allowed to use personal gallery (/gallery + uploads/albums APIs).
-- Super admin is configured in env (GALLERY_SUPER_ADMIN_EMAIL) and always allowed.
CREATE TABLE IF NOT EXISTS gallery_access_emails (
    email VARCHAR(320) PRIMARY KEY,
    note TEXT NOT NULL DEFAULT '',
    created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gallery_access_emails_created
    ON gallery_access_emails (created_at DESC);
