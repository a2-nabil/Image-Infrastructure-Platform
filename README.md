# Image Infrastructure Platform

A full-stack image platform that stores, deduplicates, and transforms uploads via a Go API, then identifies objects in those images with Gemini through a Next.js frontend.

## Architecture

```
┌─────────────────────────┐         ┌──────────────────────────────┐
│  Object Identifier UI   │         │         Image API            │
│  Next.js (apps/)        │ ──────► │  Go HTTP service             │
│  - Upload + preview     │         │  - Auth (Google / JWT)       │
│  - /api/identify        │         │  - Upload + fingerprinting   │
│    (Gemini)             │         │  - Variant generation        │
└─────────────────────────┘         │  - Rate limiting             │
                                    └──────────────┬───────────────┘
                                                   │
                     ┌─────────────────────────────┼─────────────────────────────┐
                     ▼                             ▼                             ▼
              PostgreSQL 16                   Redis 7                      AWS S3
           (metadata, users)            (rate-limit counters)         (originals + WebP
                                                                      variants)
```

| Layer | Path | Role |
|-------|------|------|
| Frontend | `apps/object-identifier` | Upload UI, calls Image API + Gemini identify |
| Backend | `services/image-api` | REST API, processing, storage, auth |
| Infra (local) | `infra/docker/docker-compose.yml` | Postgres + Redis |
| Deploy | `deploy/k8s/` | Deployment, Service, HPA, ConfigMap |
| API docs | `docs/api/v1-contract.md` | Baseline upload contract |

### Upload pipeline

1. Client sends `multipart/form-data` with field `image` to `POST /api/v1/images`.
2. API validates MIME type (`jpeg` / `png` / `webp`) and size (max **10 MB**).
3. SHA-256 fingerprint is computed. If a ready image with the same hash exists, variants are returned immediately (**deduplication**).
4. Otherwise the original is uploaded to S3, metadata is written with status `processing`, and a **Redis queue job** is enqueued. Upload returns immediately (empty `variants`).
5. In-process **variant workers** (`BRPOP` consumers) generate WebP presets, persist them, and mark the image `ready`. Clients poll `GET /api/v1/images/{id}` until `status` is `ready` or `failed`.
6. Guest uploads get a guest session ID (`X-Guest-Session-ID`) and a **72-hour** retention TTL. Authenticated uploads are owned by the user. A background **guest expiry sweeper** (every 15 minutes) deletes expired guest originals + variants from S3, then removes the Postgres rows (`image_variants` cascade).

### Variant presets

| Preset | Transform |
|--------|-----------|
| `thumbnail` | 150×150 center crop |
| `small` | Width 320 (aspect preserved) |
| `medium` | Width 768 (aspect preserved) |

All variants are encoded as WebP (quality 80).

### Object identification flow

The Next.js app runs **upload and identify in parallel**. Whichever finishes first paints first; variants keep polling in the background while identify may already be visible.

1. `POST` to the Go Image API (falls back to a local mock if the API is down); if `status=processing`, poll `GET /api/v1/images/{id}` until workers finish.
2. In parallel, `POST /api/identify` calls Gemini (`gemini-3.1-flash-lite`) and returns structured JSON (`object_name`, `confidence_score`, `summary`, etc.).

Frontend upload limit for identify: **4 MB** (JPEG / PNG / WebP).

## Prerequisites

- **Docker Desktop** running (required before Compose)
- **Go** 1.23+ (module may request a newer toolchain; Go downloads it automatically when needed)
- **Node.js** 20+ and npm
- **AWS** credentials with access to an S3 bucket
- **Gemini API key** (for object identification)
- Optional: [golang-migrate](https://github.com/golang-migrate/migrate) CLI for manual migrations
- Optional: [pre-commit](https://pre-commit.com/) + [gitleaks](https://github.com/gitleaks/gitleaks) for local hooks

## CI/CD & security gates

```bash
make install-hooks   # pre-commit (secrets, gofmt, lint)
make ci-local        # local CI subset
```

GitHub Actions pipelines: **CI → CodeQL → SAST → Staging → DAST → Production**.  
Full setup (branch protection, Environments, secrets): [`docs/ci-cd.md`](docs/ci-cd.md).

Staging/production **must** set `JWT_SECRET`, `CORS_ALLOWED_ORIGINS`, and `POSTGRES_SSLMODE=require`.

## Quick start

### 1. Environment

```bash
cp .env.example .env
cp apps/object-identifier/.env.local.example apps/object-identifier/.env.local
```

Ensure `.env` ends with a trailing newline (missing newlines can concatenate the last two variables).

Fill in at least:

| Variable | Where | Purpose |
|----------|--------|---------|
| `POSTGRES_*` | `.env` | Database connection (defaults match Compose) |
| `REDIS_URL` or `REDIS_HOST`/`REDIS_PORT` | `.env` | Rate limiting |
| `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_S3_DEV_BUCKET` | `.env` | S3 storage |
| `JWT_SECRET` | `.env` | Auth tokens (required in staging/production) |
| `CORS_ALLOWED_ORIGINS` | `.env` | Comma-separated browser origins (required in staging/production) |
| `GOOGLE_CLIENT_ID` | `.env` | Google sign-in (optional) |
| `GEMINI_API_KEY` | `.env.local` | Object identification |
| `NEXT_PUBLIC_IMAGE_API_URL` | `.env.local` | Defaults to `http://localhost:8080/api/v1` |

Local Compose defaults (already in `.env.example`):

| Key | Value |
|-----|--------|
| `POSTGRES_USER` | `postgres` |
| `POSTGRES_PASSWORD` | `postgres` |
| `POSTGRES_DB` | `image_platform_dev` |

### 2. Start dependencies

Start **Docker Desktop**, then from the repo root:

```bash
docker compose -f infra/docker/docker-compose.yml up -d
docker compose -f infra/docker/docker-compose.yml ps
```

This starts Postgres on `5432` and Redis on `6379`. Both containers should show `healthy` before continuing.

### 3. Run the Image API

```bash
cd services/image-api
go run ./cmd/server
```

In `APP_ENV=development` (default), migrations under `services/image-api/migrations` run automatically on startup.

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/readyz` must report `database`, `redis`, and `storage` as `ok` before uploads will succeed.

### 4. Run the frontend

```bash
cd apps/object-identifier
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

## Manual testing

### A. Automated smoke test (recommended)

With Docker, Image API, and (optionally) Next.js running:

```bash
# Generate a sample JPEG/PNG if needed (Windows)
powershell -File tests/fixtures/gen-images.ps1

bash scripts/smoke-test.sh
```

The script checks `/healthz`, `/readyz`, upload, variant fetch, and deduplication. If Next.js is up, it also probes `/api/identify`.

### B. End-to-end in the browser

1. Ensure Postgres, Redis, Image API, and Next.js are running (steps above).
2. Confirm `GEMINI_API_KEY` is set in `apps/object-identifier/.env.local`.
3. Open http://localhost:3000.
4. Upload a JPEG, PNG, or WebP under 4 MB.
5. Expect:
   - Image preview
   - **Image Variants** (`thumbnail`, `small`, `medium`) if the Go API is healthy
   - AI identification result (name, confidence, summary, details, actions)
6. Upload the **same file again** — you should see an **Instant Deduplication Hit**.

If the Go API is down, the UI shows an amber note and still runs Gemini identify (upload is mocked).

### C. Image API with curl

A ready fixture lives at `tests/fixtures/sample.jpg` (create it with `gen-images.ps1` if missing).

**Liveness / readiness**

```bash
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
```

**Upload (guest)**

```bash
curl -s -D - -X POST http://localhost:8080/api/v1/images \
  -H "X-Request-ID: $(uuidgen 2>/dev/null || echo test-req-1)" \
  -F "image=@./tests/fixtures/sample.jpg;type=image/jpeg"
```

Success: `201 Created` (or `200 OK` on dedupe) with `id`, `fingerprint`, `variants`, etc. Guest responses may include `X-Guest-Session-ID`.

**Fetch a variant**

```bash
# Replace IMAGE_ID and preset (thumbnail|small|medium)
curl -s -o thumb.webp -w "%{http_code}\n" \
  http://localhost:8080/api/v1/images/IMAGE_ID/variants/thumbnail
```

**Dev JWT** (only when `APP_ENV=development`)

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/dev-token \
  -H "Content-Type: application/json" \
  -d '{"user_id":"00000000-0000-0000-0000-000000000001","email":"dev@example.com"}'
```

Use the returned token:

```bash
curl -s -X POST http://localhost:8080/api/v1/images \
  -H "Authorization: Bearer YOUR_JWT" \
  -F "image=@./tests/fixtures/sample.jpg;type=image/jpeg"
```

**Identify only (Next.js route)**

```bash
curl -s -X POST http://localhost:3000/api/identify \
  -F "image=@./tests/fixtures/sample.jpg;type=image/jpeg"
```

### D. Smoke checklist

| Check | How |
|-------|-----|
| Infra up | `docker compose -f infra/docker/docker-compose.yml ps` → healthy |
| API ready | `curl http://localhost:8080/readyz` → all `ok` |
| Upload works | `bash scripts/smoke-test.sh` or UI → variants returned |
| Dedup works | Upload same file twice → `deduplicated: true` |
| Identify works | UI or `/api/identify` → structured Gemini JSON |
| Rate limit | >10 uploads / minute → HTTP 429 on upload |

## API surface

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| `GET` | `/healthz` | — | Liveness |
| `GET` | `/readyz` | — | Postgres + Redis + S3 |
| `POST` | `/api/v1/images` | Optional JWT | Upload; enqueues async variants; 10 req/min (Redis) |
| `GET` | `/api/v1/images/{id}` | Owner JWT or `X-Guest-Session-ID` | Poll metadata + variants (`processing` → `ready`) |
| `GET` | `/api/v1/images/{id}/variants/{preset}` | Owner JWT or `X-Guest-Session-ID` | Stream variant; 60 req/min |
| `GET` | `/metrics` | — | Prometheus metrics |
| `POST` | `/api/v1/auth/google` | — | Exchange Google ID token → JWT |
| `POST` | `/api/v1/auth/dev-token` | — | Dev-only JWT mint |
| `POST` | `/api/v1/users/claim-guest-media` | Required JWT | Claim guest uploads |

Full upload contract: [`docs/api/v1-contract.md`](docs/api/v1-contract.md).

## Project structure

```
.
├── apps/object-identifier/     # Next.js UI + Gemini identify route
├── services/image-api/         # Go API
│   ├── cmd/server/             # Entrypoint
│   ├── internal/               # handlers, services, queue, worker, storage, middleware
│   ├── migrations/             # SQL migrations
│   └── Dockerfile
├── infra/docker/               # Local Postgres + Redis
├── deploy/k8s/                 # Kubernetes manifests
├── docs/api/                   # API contracts
├── scripts/smoke-test.sh       # Local API smoke test
├── tests/fixtures/             # Sample images for manual/curl tests
├── Makefile                    # migrate-up / migrate-down
└── .env.example
```

## Database migrations

Auto-applied in development on API startup. Manually:

```bash
# Requires migrate CLI and a filled .env
make migrate-up
make migrate-down
```

## Kubernetes

Manifests live in `deploy/k8s/`:

- `deployment.yaml` — 2 replicas, probes, non-root, read-only root FS, Prometheus scrape annotations
- `serviceaccount.yaml` — IRSA annotation for S3 access (no long-lived AWS keys in the pod)
- `service.yaml` — ClusterIP → port 8080
- `hpa.yaml` — scale 2–10 on 75% CPU
- `configmap.yaml` / `secret.yaml.example` — config and secrets

Copy and fill `secret.yaml.example` before applying; never commit real secrets. On EKS, set the ServiceAccount role ARN and leave `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` unset.

## Implementation notes

- **Ownership**: `GET` image/variant requires matching JWT `user_id` or `X-Guest-Session-ID`. Deduplication is tenant-scoped.
- **Presigned URLs**: set `AWS_S3_PRESIGN_URLS=true` to return short-lived S3 GET URLs after ownership checks (`AWS_S3_PRESIGN_TTL_SECONDS`, default 900).
- **IRSA**: staging/production may omit static AWS keys and use the default credential chain (EKS IRSA). Development still requires static keys unless you override carefully.
- **Access logs + metrics**: structured `http_access` logs and Prometheus text on `GET /metrics`.
- **Deduplication** is content-addressed via SHA-256 within the same owner/guest session.
- **CORS** and **X-Request-ID** middleware wrap all routes; request IDs are echoed when supplied.
- **Rate limits** use Redis fixed windows (`upload`: 10/min, `variants`: 60/min).
- **Storage paths**: authenticated → `users/{userID}/...`; guests → `temp/...`.
- **CDN**: when `CDN_BASE_URL` is a non-localhost origin and presign is off, variant URLs point at the CDN; otherwise they use the API proxy path or presigned S3 URLs.
- Frontend stores `X-Guest-Session-ID` in localStorage and sends it on upload/poll so ownership checks pass.

## Troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| Compose fails / daemon error | Docker Desktop not running |
| `/readyz` storage error | Invalid AWS credentials, empty keys, or wrong bucket name |
| `/readyz` database error | Postgres not healthy or wrong `POSTGRES_*` |
| UI amber “Go Image API unavailable” | API not running or wrong `NEXT_PUBLIC_IMAGE_API_URL` |
| Identify 500 `MISSING_API_KEY` | `GEMINI_API_KEY` missing in `.env.local` |
| Upload 429 | Hit rate limit; wait or clear Redis keys |
| Upload 413 / `FILE_TOO_LARGE` | File over 10 MB (API) or 4 MB (UI identify) |
| Env vars look concatenated | `.env` missing trailing newline after last line |

## License

Private / unrestricted unless otherwise specified by the repository owner.
