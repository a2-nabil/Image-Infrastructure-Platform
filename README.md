# Image Infrastructure Platform

Production-shaped practice stack for image upload, processing, AI identification, and a personal Gallery.

| Layer | Stack |
|-------|--------|
| API | Go (`services/api`) — JWT auth, ownership, gallery allowlist, S3, Postgres, Redis |
| Web | Next.js 16 (`apps/web`) — Converter, Identify, Dashboard, Gallery |
| Data | Postgres (metadata), Redis (rate limits / queues), S3 (objects) |
| AI | Google Gemini (optional) for object identification |

## What you can do

| Surface | Who | Purpose |
|---------|-----|---------|
| **Converter** | Anyone (guest or signed-in) | Format conversion + download; tools library |
| **Identify** | Anyone | Upload → Gemini labels / description |
| **Dashboard** | Signed-in | Your **tools** library (converter / identify history) — not Gallery |
| **Gallery** | Super admin + allowlisted emails only | Google Photos–style personal library (albums, trash, chunked upload) |

Gallery media uses `library_scope=gallery` and never appears in Dashboard or `GET /users/me/images`. Tools uploads use `library_scope=tools`.

## Security (read this)

Full detail: **[docs/security.md](docs/security.md)**.

- **Server-enforced ownership** on every media route (JWT user, guest session, or short-lived tenant-bound capability URL).
- **Gallery allowlist**: only `a2nabil000@gmail.com` (or `GALLERY_SUPER_ADMIN_EMAIL`) and emails they grant may call gallery APIs or open `/gallery`. Everyone else gets Converter / Identify / Dashboard only (`403 GALLERY_FORBIDDEN` on gallery routes).
- **Guest claim** cannot spoof another browser’s guest id (must match bound session header/cookie).
- **Capability URLs** bind image id + tenant; ~1h TTL.
- **No cross-tenant dedupe**; soft-revalidate never keeps another user’s grid when the server returns `total=0`.
- Do not commit secrets — use `.env.example` / `.env.local.example` only.

## Monorepo layout

```text
apps/
  web/                 Next.js product UI (Converter, Identify, Dashboard, Gallery, …)
services/
  api/                 Go HTTP API (JWT, S3, Postgres, Redis, gallery allowlist)
  worker/              Reserved for future background workers
docs/
  security.md          Auth, ownership, gallery isolation
  deploy.md            Render, Netlify, VPS / Docker, Kubernetes
  api/v1-contract.md   HTTP contract
  beginner-setup.md    Local setup
  ci-cd.md             Pipeline
infra/                 Docker Compose, Terraform sketches
deploy/k8s/            Kubernetes manifests
.github/workflows/     CI / CD / security scans
```

## Quick start (local)

**Prerequisites:** Docker Desktop, Go 1.25+, Node 20+, make (optional).

```bash
# Infra
docker compose -f infra/docker/docker-compose.yml up -d

# API — copy env, then run
cp .env.example .env          # edit JWT_SECRET, CORS, GALLERY_SUPER_ADMIN_EMAIL, optional AWS
cd services/api && go run ./cmd/server

# Web (second terminal)
cd apps/web
cp .env.local.example .env.local   # Google client id + IMAGE_API_ORIGIN / GEMINI_API_KEY
npm install && npm run dev
```

| Service | URL |
|---------|-----|
| Web | http://localhost:3000 |
| API | http://127.0.0.1:8080 |
| Health | `GET /healthz`, `GET /readyz` |

Without real AWS credentials the API uses an **in-memory S3 stub** (fine for UI/API practice).

### Google sign-in

1. Create an OAuth Web client in Google Cloud Console.
2. Authorized JavaScript origins: `http://localhost:3000` (and your deployed origin).
3. Set `NEXT_PUBLIC_GOOGLE_CLIENT_ID` in `apps/web/.env.local`.
4. API verifies the Google ID token and issues a JWT (`JWT_SECRET` in root `.env`).

### Gallery access

1. Sign in as the super admin email (default `a2nabil000@gmail.com`).
2. Open **Gallery** from the header ⋮ menu (not the main nav).
3. Use **Access** in Gallery settings to allowlist other emails.
4. Set `GALLERY_SUPER_ADMIN_EMAIL` in `.env` if you need a different super admin for a fork/demo.

## API surface (high level)

| Area | Endpoints (prefix `/api/v1`) |
|------|------------------------------|
| Auth | `POST /auth/google`, `GET /auth/me`, `POST /auth/dev-token` (non-prod) |
| Tools media | `POST /images`, `GET /images/{id}`, variants, convert, identify |
| User tools library | `GET /users/me/images` (`library_scope=tools` only) |
| Guest claim | `POST /users/claim-guest-media` (session-bound) |
| Gallery | `POST/GET /users/me/uploads*`, albums, trash, `GET /users/me/gallery/media` — **RequireGalleryAccess** |

Contract: [docs/api/v1-contract.md](docs/api/v1-contract.md).

## Frontend routes

| Route | Access |
|-------|--------|
| `/` | Marketing |
| `/converter`, `/identify` | Public (guest or auth) |
| `/dashboard` | Signed-in — tools library |
| `/gallery` | Gallery allowlist only (client gate + API) |
| `/login` | Google sign-in |

Theme: light / dark (`a2n-theme`). Overlays use `PhotosSheet` (bottom sheet on mobile, centered modal on `md+`).

## Quality & hooks

```bash
make fmt
make lint
make test
make install-hooks   # pre-commit: gofmt, golangci-lint, gitleaks, frontend lint
```

CI also runs gitleaks, CodeQL, GoSec/Semgrep, npm audit, and staging DAST. See [docs/ci-cd.md](docs/ci-cd.md).

## Docs

| Doc | Topic |
|-----|--------|
| [docs/security.md](docs/security.md) | Ownership, gallery, guest claim, ops |
| [docs/deploy.md](docs/deploy.md) | Render, Netlify, Vercel, VPS/Docker, K8s |
| [docs/api/v1-contract.md](docs/api/v1-contract.md) | Request/response shapes |
| [docs/beginner-setup.md](docs/beginner-setup.md) | First-time setup |
| [docs/ci-cd.md](docs/ci-cd.md) | Pipelines |
| [docs/personal-interview-banglish.md](docs/personal-interview-banglish.md) | Interview talking points |
| [apps/web/README.md](apps/web/README.md) | Frontend notes |

## Disclaimer

Practice / portfolio project. Harden secrets, TLS, WAF, and IAM before any real production traffic. Gallery is intentionally private to an allowlist — do not expose it as a multi-tenant public photos product without redesigning access and quotas.
