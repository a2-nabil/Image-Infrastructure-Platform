# Deploy to the cloud

Recommended split for this monorepo:

| Piece | Path | Typical host |
|-------|------|----------------|
| **Web** (Next.js) | `apps/web` | [Netlify](https://www.netlify.com/) or Vercel |
| **API** (Go) | `services/api` | [Render](https://render.com/), Fly.io, Railway, or your own VPS/Docker |
| **Data** | Postgres + Redis + S3 | Managed DB (Render/Neon/RDS), Redis Cloud/Upstash, AWS S3 |

Local-only in-memory S3 stub is **not** for production. Use a real bucket (or compatible object store) and strong secrets.

Security checklist before going public: [docs/security.md](security.md).

---

## Architecture (Netlify + Render)

This is the path already sketched in `apps/web/netlify.toml`:

```text
Browser ──► Netlify (apps/web)
              │
              ├─ /api/identify  → Gemini (GEMINI_API_KEY on Netlify)
              └─ /api/v1/*      → rewrite/proxy → Render Go API
                                      │
                                      ├─ Postgres
                                      ├─ Redis
                                      └─ S3
```

Same-origin `/api/v1` keeps cookies, uploads, and `<img>` URLs on your Netlify domain while the Go service stays on Render.

---

## 1) API on Render

1. Create a **Web Service** from this GitHub repo.
2. Settings:

| Setting | Value |
|---------|--------|
| Root directory | `services/api` |
| Runtime | Docker (uses `services/api/Dockerfile`) **or** native Go |
| Docker | Dockerfile path `./Dockerfile` (with root dir `services/api`) |
| Health check | `/healthz` |
| Port | `8080` |

Native Go alternative (no Docker):

| Setting | Value |
|---------|--------|
| Build command | `go build -o api ./cmd/server` |
| Start command | `./api` |

3. Attach **Postgres** and **Redis** (Render add-ons or external). Put connection values in the service env.
4. Environment variables (minimum):

| Variable | Notes |
|----------|--------|
| `APP_ENV` | `staging` or `production` |
| `PORT` | `8080` |
| `SERVER_BASE_URL` | Public API URL, e.g. `https://your-api.onrender.com` |
| `CORS_ALLOWED_ORIGINS` | Your Netlify/Vercel origin(s), comma-separated |
| `JWT_SECRET` | Long random secret |
| `GOOGLE_CLIENT_ID` | Same as frontend `NEXT_PUBLIC_GOOGLE_CLIENT_ID` |
| `GALLERY_SUPER_ADMIN_EMAIL` | Gallery allowlist owner |
| `POSTGRES_*` / `REDIS_URL` | From your providers |
| `POSTGRES_SSLMODE` | `require` in staging/prod |
| `MIGRATE_ON_START` | `true` once for first boot, then prefer `false` + migrate job |
| `AWS_REGION`, `AWS_S3_DEV_BUCKET` | Real bucket |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Or instance role / IRSA off Render |
| `AWS_S3_PRESIGN_URLS` | Optional `true` after ownership is verified |

5. Confirm `GET https://your-api.onrender.com/readyz` shows database/redis/storage OK.
6. Free-tier Render services **spin down** when idle — first request after sleep can be slow. Use a paid instance for demos that must stay warm.

Optional: add a [render.yaml](https://render.com/docs/blueprint-spec) Blueprint later for reproducible services.

---

## 2) Web on Netlify

1. New site from the same GitHub repo.
2. Build settings:

| Setting | Value |
|---------|--------|
| Base directory | `apps/web` |
| Build command | `npm run build` |
| Publish | handled by `@netlify/plugin-nextjs` (see `apps/web/netlify.toml`) |

3. Environment variables on Netlify:

| Variable | Example |
|----------|---------|
| `IMAGE_API_ORIGIN` | `https://your-api.onrender.com` (no `/api/v1` suffix) |
| `NEXT_PUBLIC_IMAGE_API_URL` | `/api/v1` |
| `NEXT_PUBLIC_GOOGLE_CLIENT_ID` | Google OAuth web client ID |
| `GEMINI_API_KEY` | Server-only; never `NEXT_PUBLIC_` |

4. Update `apps/web/netlify.toml` redirects so `/api/v1/*` proxies to your Render URL (replace the example host).
5. In Google Cloud Console, add your Netlify URL to **Authorized JavaScript origins** (and redirect URIs if you use them).
6. Set API `CORS_ALLOWED_ORIGINS` to that same Netlify origin.

Deploy preview tip: either restrict CORS to production only, or add preview URLs carefully — do not leave CORS as `*`.

---

## 3) Web on Vercel (alternative)

| Setting | Value |
|---------|--------|
| Root directory | `apps/web` |
| Framework | Next.js |

Env: same as Netlify (`IMAGE_API_ORIGIN`, `NEXT_PUBLIC_IMAGE_API_URL=/api/v1`, Google + Gemini).

Add a rewrite in `apps/web/next.config.ts` (already present) **or** Vercel rewrites so `/api/v1/:path*` → `$IMAGE_API_ORIGIN/api/v1/:path*`. Keep `/api/identify` on the Next app.

---

## 4) Own VPS / Docker (API + optional Caddy)

On a Linux VPS (Ubuntu example):

```bash
# Install Docker, then from the repo:
docker build -t platform-api:local ./services/api

docker run -d --name platform-api \
  -p 8080:8080 \
  --env-file .env \
  platform-api:local
```

Put Postgres/Redis on the same host (Docker Compose) or use managed services. Terminate TLS with Caddy/Nginx:

```caddy
api.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

Point the web host’s `IMAGE_API_ORIGIN` at `https://api.example.com`.

Compose sketch (API only; add db/redis services as needed):

```yaml
services:
  api:
    build: ./services/api
    ports: ["8080:8080"]
    env_file: [.env]
    restart: unless-stopped
```

---

## 5) Kubernetes

Manifests live under `deploy/k8s/` (Deployment still named `image-api` for the cluster resource). Build/push the image from `services/api`, set ConfigMap/Secret, then apply. See [docs/beginner-setup.md](beginner-setup.md) and [docs/ci-cd.md](ci-cd.md) for staging/production GitHub Actions + GHCR.

---

## Environment matrix

| Concern | Local | Cloud |
|---------|-------|-------|
| Web URL | `http://localhost:3000` | Netlify/Vercel HTTPS |
| API URL | `http://127.0.0.1:8080` | Render / VPS HTTPS |
| `NEXT_PUBLIC_IMAGE_API_URL` | `/api/v1` | `/api/v1` (same-origin proxy) |
| `IMAGE_API_ORIGIN` | `http://localhost:8080` | Public API origin |
| S3 | optional stub | real bucket |
| `APP_ENV` | `development` | `staging` / `production` |
| Secrets | `.env` / `.env.local` (gitignored) | Host env / secret manager |

Never commit `.env`, `.env.local`, AWS keys, JWT secrets, or Gemini keys.

---

## Smoke after deploy

```bash
curl -sS https://YOUR_API/healthz
curl -sS https://YOUR_API/readyz
curl -sS -o /dev/null -w "%{http_code}\n" https://YOUR_WEB/
curl -sS -o /dev/null -w "%{http_code}\n" https://YOUR_WEB/api/v1/healthz
```

Sign in with Google, upload via Converter, confirm Dashboard tools library. Gallery remains allowlist-only.

---

## Adding more features later

- New UI routes stay under `apps/web` (or add another app under `apps/` if you split products).
- New HTTP APIs stay in `services/api`, or add `services/<name>` beside it.
- Background jobs can land in `services/worker` when you outgrow in-process workers.
