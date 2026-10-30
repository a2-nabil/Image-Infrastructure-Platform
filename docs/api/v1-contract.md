# Image API v1 Contract

Baseline HTTP contract for `services/api`. All JSON success envelopes use `{ "success": true, "data": ... }`. Errors use `{ "success": false, "error": { "code", "message" } }`.

Security model: [docs/security.md](../security.md).

## Common headers

| Header | When |
|--------|------|
| `Authorization: Bearer <JWT>` | Authenticated routes |
| `X-Guest-Session-ID` | Guest tools uploads / claim binding |
| `X-Request-ID` | Optional correlation (echoed or generated) |

## Health

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| `GET` | `/healthz` | none | Liveness |
| `GET` | `/readyz` | none | DB + Redis + S3 |
| `GET` | `/metrics` | none | Prometheus |

---

## Auth

### `POST /api/v1/auth/google`

Body: `{ "id_token": "<Google ID token>" }` → JWT + user profile.

### `POST /api/v1/auth/dev-token`

Development only. Disabled when `APP_ENV=production`.

### `GET /api/v1/users/me`

Requires JWT. Returns profile including gallery access flags when applicable.

---

## Tools media (`library_scope=tools`)

### `POST /api/v1/images`

Upload an image for processing and metadata extraction.

| Item | Value |
|------|--------|
| Content-Type | `multipart/form-data` |
| Form field | `image` (required) |
| Auth | Optional (JWT user or guest session) |
| Rate limit | ~10/min (Redis) |

#### Success — `201 Created` (new) / `200 OK` (dedupe)

New uploads may return `status: "processing"` with `variants: []` while workers generate WebP presets. Poll `GET /api/v1/images/{id}` until `ready` or `failed`. Deduplicated uploads return `status: "ready"` with `deduplicated: true`.

Dedupe is **tenant- and library-scope scoped** (never cross-user).

```json
{
  "success": true,
  "data": {
    "id": "uuid",
    "fingerprint": "sha256_hash",
    "original_url": "string",
    "width": 0,
    "height": 0,
    "format": "string",
    "size_bytes": 0,
    "status": "processing",
    "deduplicated": false,
    "library_scope": "tools",
    "variants": []
  }
}
```

### `GET /api/v1/images/{id}`

Poll metadata and variants. Ownership required (JWT user, matching guest, or valid capability query).

### `GET /api/v1/images/{id}/original`

Stream / redirect original. Same ownership rules. Capability URLs use `?exp=&sig=` bound to **image id + tenant** (~1h TTL).

### `GET /api/v1/images/{id}/variants/{preset}`

Stream WebP preset (`thumbnail` / `small` / `medium`). Ownership enforced.

### User tools library

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| `GET` | `/api/v1/users/me/images` | JWT | **tools** scope only — never gallery |
| `POST` | `/api/v1/users/me/images/trash` | JWT | Soft-delete |
| `POST` | `/api/v1/users/me/images/restore` | JWT | Restore from trash |
| `POST` | `/api/v1/users/me/images/purge` | JWT | Permanent delete |
| `DELETE` | `/api/v1/users/me/images/{id}` | JWT | Delete one |
| `DELETE` | `/api/v1/users/me/images/{id}/variants/{preset}` | JWT | Delete variant |

### `POST /api/v1/users/claim-guest-media`

Requires JWT. Moves guest tools uploads to the signed-in user.

- Body guest id (if sent) **must match** bound `X-Guest-Session-ID` / cookie.
- Mismatch → `403` (cannot claim another browser’s guest media).

---

## Identify

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| `POST` | `/api/v1/identify/lookup` | none / optional | Cache lookup |
| `POST` | `/api/v1/identify/store` | optional | Persist identification |

Frontend also calls Next.js `POST /api/identify` (Gemini key server-side only).

---

## Personal Gallery (`library_scope=gallery`)

**All routes below require `RequireGalleryAccess`**: JWT + (super admin email **or** row in `gallery_access_emails`).

Failure → `403` with code `GALLERY_FORBIDDEN`.

Super admin default: `a2nabil000@gmail.com` (`GALLERY_SUPER_ADMIN_EMAIL`).

Gallery objects use S3 prefix `users/{id}/gallery/...` and **do not** appear in `GET /users/me/images`.

### Media list

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/api/v1/users/me/gallery/media` | Gallery library only |

### Chunked upload

| Method | Path | Rate limit | Notes |
|--------|------|------------|-------|
| `POST` | `/api/v1/users/me/uploads` | ~60/min/user | Init multipart session |
| `PUT` | `/api/v1/users/me/uploads/{id}/chunks/{index}` | ~600/min/user | Upload part; etags merge atomically |
| `POST` | `/api/v1/users/me/uploads/{id}/complete` | ~60/min/user | Assemble + register media |
| `DELETE` | `/api/v1/users/me/uploads/{id}` | — | Abort |

### Albums

| Method | Path |
|--------|------|
| `GET` | `/api/v1/users/me/albums` |
| `POST` | `/api/v1/users/me/albums` |
| `PATCH` | `/api/v1/users/me/albums/{id}` |
| `DELETE` | `/api/v1/users/me/albums/{id}` |
| `POST` | `/api/v1/users/me/albums/{id}/items` |
| `DELETE` | `/api/v1/users/me/albums/{id}/items` |

### Gallery allowlist admin

Super-admin only (handler checks role after auth):

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/api/v1/admin/gallery-access` | List allowed emails |
| `POST` | `/api/v1/admin/gallery-access` | Allow email |
| `POST` | `/api/v1/admin/gallery-access/revoke` | Revoke email |

---

## Error shape

```json
{
  "success": false,
  "error": {
    "code": "GALLERY_FORBIDDEN",
    "message": "string"
  }
}
```

Common codes: `UNAUTHORIZED`, `FORBIDDEN`, `GALLERY_FORBIDDEN`, `NOT_FOUND`, `VALIDATION_ERROR`, `RATE_LIMITED`, `INTERNAL`.
