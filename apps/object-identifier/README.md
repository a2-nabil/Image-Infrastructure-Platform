# Object Identifier

Next.js frontend for the [Image Infrastructure Platform](../../README.md).

Runs **upload** (Go Image API) and **identify** (Gemini via `POST /api/identify`) in parallel so the UI can show whichever response finishes first. After upload returns `processing`, the app polls `GET /api/v1/images/{id}` until async workers produce WebP variants.

## What this app needs

| Dependency | Required for | Default |
|------------|--------------|---------|
| Image API (`:8080`) | Variants, dedupe, storage metadata | `http://localhost:8080/api/v1` |
| Postgres + Redis | Image API itself | via root Docker Compose |
| AWS S3 | Persisting originals/variants | from root `.env` |
| `GEMINI_API_KEY` | Object identification | set in `.env.local` |

Without the Image API, the UI still runs identify (upload is mocked and an amber note appears). Without `GEMINI_API_KEY`, identify returns `MISSING_API_KEY`.

## Environment

```bash
cp .env.local.example .env.local
```

| Variable | Required | Description |
|----------|----------|-------------|
| `GEMINI_API_KEY` | Yes (for identify) | Google Gemini API key |
| `NEXT_PUBLIC_IMAGE_API_URL` | No | Defaults to same-origin `/api/v1` (Netlify/`next.config` rewrite) |

## Run (frontend only)

```bash
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

Upload limits in the UI: JPEG / PNG / WebP, max **4 MB**.

## Full local stack

From the **repo root** (Docker Desktop must be running):

```bash
cp .env.example .env          # fill AWS keys + keep Postgres defaults
docker compose -f infra/docker/docker-compose.yml up -d

cd services/image-api && go run ./cmd/server
# in another terminal:
cd apps/object-identifier && npm run dev
```

Then open http://localhost:3000 and upload an image, or run:

```bash
bash scripts/smoke-test.sh
```

See the root [README](../../README.md) for architecture, API surface, curl examples, and troubleshooting.
