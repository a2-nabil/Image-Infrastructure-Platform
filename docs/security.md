# Security model

This document describes how ownership, gallery isolation, and auth are enforced.
UI hiding is never enough — the API always decides access.

## Principles

1. **Server-side enforcement** for every media and gallery route.
2. **Tenant isolation** — users never list or mutate another account’s library.
3. **Separate libraries** — Converter / Identify / Dashboard (`tools`) vs personal Gallery (`gallery`).
4. **Least privilege** — personal Gallery is allowlist-only (plus a fixed super admin).
5. **No secrets in the frontend** — only `NEXT_PUBLIC_*` and public Google client IDs.

## Authentication

| Mechanism | Use |
|-----------|-----|
| Google ID token → JWT (`POST /api/v1/auth/google`) | Production sign-in |
| `Authorization: Bearer <JWT>` | Authenticated API calls |
| `X-Guest-Session-ID` / guest cookie | Ephemeral guest uploads (tools library only) |
| `POST /api/v1/auth/dev-token` | **Development only** — disabled when `APP_ENV=production` |

Staging/production **must** set a strong `JWT_SECRET`, `CORS_ALLOWED_ORIGINS`, and `POSTGRES_SSLMODE=require`.

## Ownership (tools media)

- Authenticated uploads store `user_id` and live under `library_scope=tools`.
- Guest uploads store `guest_session_id` + `expires_at` (~72h) and are swept automatically.
- `GET` image / original / variant requires:
  - matching JWT `user_id`, or
  - matching guest session, or
  - a short-lived tenant-bound capability URL (`?exp=&sig=`).
- Capability signatures include **image id + tenant** (`u:<userId>` or `g:<guestSession>`) so a leaked URL from another account’s session cannot authorize a different owner’s object (TTL ~1 hour).
- Deduplication is **tenant- and library-scope scoped** (never cross-user).

## Guest claim

- `POST /api/v1/users/claim-guest-media` requires auth.
- The body guest id (if present) **must match** the bound `X-Guest-Session-ID` / cookie — clients cannot claim another browser’s guest uploads by forging an id.
- After claim, the frontend rotates the guest session id.

## Personal Gallery (allowlist)

| Rule | Detail |
|------|--------|
| Super admin | `a2nabil000@gmail.com` (override with `GALLERY_SUPER_ADMIN_EMAIL`) |
| Allowlist | Table `gallery_access_emails`, managed only by the super admin |
| UI | `/gallery` nav entry and Access settings only for allowed users |
| API | All `/users/me/uploads*`, `/users/me/albums*`, `/users/me/gallery/media` require `RequireGalleryAccess` |
| Data | Rows use `library_scope=gallery` and S3 prefix `users/{id}/gallery/...` |
| Dashboard | `GET /users/me/images` returns **tools** scope only — never gallery originals |

Non-allowlisted signed-in users still use Converter, Identify, and Dashboard. They must not see Gallery links or succeed on gallery APIs (expect `403 GALLERY_FORBIDDEN`).

## Account switch / cache

- Gallery and dashboard caches are keyed by user id + library kind.
- Logout / account switch clears session, guest id, and sessionStorage gallery caches.
- Soft-revalidate never keeps more grid rows than the server `total` (prevents cross-account UI flashes).

## Uploads (gallery)

- Chunked multipart to S3 (auth + gallery access required).
- Concurrent chunk metadata merges **atomically** in Postgres (`part_etags || patch`) to avoid lost updates.
- Rate limits (per user): init ~60/min, chunks ~600/min.
- Quotas: pending sessions, daily file/byte caps (see `gallery_upload.go`).

## Operational security

- Pre-commit: gitleaks + format/lint (`make install-hooks`).
- CI: gitleaks, Go tests, Next build, CodeQL, GoSec/Semgrep, npm audit, DAST on staging.
- Never commit `.env`, AWS keys, JWT secrets, or Gemini keys — use `.env.example` / `.env.local.example` only.
- Prefer IRSA in EKS; avoid long-lived AWS keys in pods.

## Quick defensive checks

```bash
# Unauthenticated tools list → 401
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/api/v1/users/me/images

# Unauthenticated gallery upload → 401
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8080/api/v1/users/me/uploads \
  -H 'Content-Type: application/json' -d '{}'

# Mismatched guest claim → 403 (with a valid JWT + different X-Guest-Session-ID)
```

See also [`docs/ci-cd.md`](ci-cd.md) and the root [README](../README.md).
