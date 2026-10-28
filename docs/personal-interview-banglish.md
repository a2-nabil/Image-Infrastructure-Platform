# Image Infrastructure Platform — Personal A2Z Guide (Banglish)

> Ei file amar personal notes — interview / presentation e explain korar jonno.  
> Language: Banglish (Bangla + English mix). Focus: **SRS, stack, code, keno ki use korlam**.

---

## 1) Project one-liner (elevator pitch)

Ami ekta **full-stack Image Infrastructure Platform** banaisi jekhane:

1. User image upload kore  
2. Backend image **store** kore (S3), **dedupe** kore (SHA-256), **variants** banay (thumbnail/small/medium WebP)  
3. Frontend Gemini diye image er **main object identify** kore  

Interview e bolte paro:

> “I built an image platform with a Go REST API for upload/processing/storage, Postgres for metadata, Redis for rate limiting, S3 for objects, and a Next.js app that uses Gemini for object identification — with Docker locally and Kubernetes + GitHub Actions for deployment/security gates.”

---

## 2) Problem / keno ei project?

Real world e image system e common challenges:

| Problem | Solution ami implement korlam |
|---------|--------------------------------|
| Same image bar bar upload → storage waste | SHA-256 **content fingerprint** + dedupe |
| Mobile/web e different sizes lage | On-upload **variant generation** (thumbnail/small/medium) |
| Guest user o upload korte chay | Guest session + 72h TTL concept |
| Abuse / spam upload | Redis **rate limiting** |
| “Ei image e ki ache?” | Gemini vision via Next.js `/api/identify` |
| Prod e blind deploy | CI → CodeQL → SAST → Staging → DAST → Production |

---

## 3) SRS style — requirements (interview friendly)

### 3.1 Functional requirements (system ki korte pare)

1. User JPEG/PNG/WebP upload korte parbe (`POST /api/v1/images`)  
2. System MIME + size validate korbe (API max **10MB**)  
3. Same content hole dedupe hit dekhabe (`deduplicated: true`)  
4. 3 ta WebP variant generate hobe: thumbnail (150×150), small (320w), medium (768w)  
5. Variant serve hobe: `GET /api/v1/images/{id}/variants/{preset}`  
6. Optional Google auth + JWT; guest upload o possible  
7. Guest media claim: login er por `claim-guest-media`  
8. Frontend e AI identify: object name, confidence, summary, key details, suggested actions  
9. Health endpoints: `/healthz` (alive), `/readyz` (DB+Redis+S3 ready)

### 3.2 Non-functional requirements

| Area | Requirement | Ami ki korechi |
|------|-------------|----------------|
| Performance | Dedupe + variants | Fingerprint lookup; WebP quality 80 |
| Scalability | Horizontal scale | K8s Deployment + HPA (CPU 75%) |
| Reliability | Graceful shutdown | SIGTERM/SIGINT, 10s shutdown |
| Security | Auth, limits, scan | JWT/Google, rate limit, MaxBytesReader, SAST/CodeQL |
| Observability (basic) | Correlation | `X-Request-ID` middleware |
| Deployability | Local + cloud path | Docker Compose local; K8s manifests; GH Actions |

### 3.3 Out of scope / incomplete (honest bolte paro)

Interview e honesty plus:

- Guest TTL cleanup: sweeper deletes expired guest S3 objects + DB rows  
- Variant URL e ownership check weak  
- Cross-tenant dedupe leakage risk  
- Full metrics/tracing (Prometheus/OTel) nei  
- Presigned S3 URL nei  

> “MVP e core pipeline complete; production hardening partially done — CI/security gates + CORS/JWT/SSL rules add korechi, remaining items backlog e.”

---

## 4) High-level architecture

```
Browser (Next.js :3000)
   │
   ├─► Go Image API :8080  ──► PostgreSQL (metadata, users)
   │         │               ──► Redis (rate limit counters)
   │         └───────────────► AWS S3 (raw + variants)
   │
   └─► Next.js /api/identify ──► Google Gemini
```

**Folder map (monorepo):**

| Path | Role |
|------|------|
| `apps/object-identifier/` | Next.js frontend + Gemini route |
| `services/image-api/` | Go backend API |
| `infra/docker/` | Local Postgres + Redis |
| `deploy/k8s/` | Kubernetes manifests |
| `.github/workflows/` | CI/CD + security pipelines |
| `docs/` | API contract, CI guide, beginner guide |
| `scripts/` | smoke-test, pre-commit, ci-local |

---

## 5) Tech stack — ki use korlam + **KENO**

### 5.1 Backend: **Go**

**Keno Go?**
- Fast HTTP services, simple concurrency  
- Single binary deploy (Docker e easy)  
- Strong stdlib (`net/http`, `database/sql`, `crypto/sha256`)  
- Image processing libs mature (`imaging`, `webp`)

**Key packages:**
- `aws-sdk-go-v2` → S3 upload/get  
- `lib/pq` → Postgres driver  
- `go-redis` → rate limit  
- `golang-jwt` → JWT  
- `google.golang.org/api/idtoken` → Google ID token verify  
- `disintegration/imaging` + `chai2010/webp` → resize + encode  

### 5.2 Frontend: **Next.js 16 + React 19 + TypeScript + Tailwind**

**Keno Next.js?**
- App Router + API routes (`/api/identify`) — Gemini key **server-side** thake (browser e leak hoy na)  
- Fast UI for upload/preview  
- TypeScript type safety  

**Keno Gemini frontend route e?**
- AI call alada concern; image infra API clean thake  
- Key `GEMINI_API_KEY` only `.env.local` / server env e  

### 5.3 Database: **PostgreSQL 16**

**Keno Postgres?**
- Relational metadata: images, variants, users  
- Migrations with golang-migrate  
- Fingerprint index for dedupe lookup  

Tables (conceptually):
- `users` — Google identity  
- `images` — fingerprint, storage_path, size, status, user/guest, expires_at, location  
- `image_variants` — preset_name, dimensions, storage_path  

### 5.4 Cache / limiter: **Redis 7**

**Keno Redis?**
- Rate limit counters (fixed window)  
- Upload: 10/min, Variants: 60/min  
- Fast in-memory, multi-instance API te shared limit  

### 5.5 Object storage: **AWS S3**

**Keno S3?**
- Durable blob storage for originals + variants  
- Path design:  
  - Guest: `temp/raw/...`, `temp/variants/...`  
  - User: `users/{userID}/...`  

### 5.6 AI: **Google Gemini** (`gemini-3.1-flash-lite`)

**Keno Gemini?**
- Vision + structured JSON response  
- Schema diye object_name, confidence, summary etc. force kori  

### 5.7 Local infra: **Docker Compose**

Postgres + Redis containers — “works on my machine” problem komay.

### 5.8 Deploy: **Kubernetes**

Deployment, Service, HPA, ConfigMap, Secret example.  
Production ideas: non-root container, probes `/healthz` `/readyz`, rolling update.

### 5.9 CI/CD & security

| Tool | Keno |
|------|------|
| GitHub Actions CI | Build/test every push |
| CodeQL | Semantic security analysis |
| GoSec + Semgrep + npm audit | SAST |
| Gitleaks / pre-commit | Secret leak block |
| OWASP ZAP (DAST) | Staging URL attack surface |
| Staging/Production workflows | Promote image + optional kubectl |

---

## 6) Backend code structure (Go layered architecture)

```
services/image-api/
  cmd/server/main.go          ← entrypoint, wiring, graceful shutdown
  internal/
    config/                   ← env load + validation
    handler/                  ← HTTP layer (request/response)
    service/                  ← business logic (upload, variants, user)
    storage/                  ← S3 client
    database/                 ← Postgres + migrations
    middleware/               ← CORS, auth, rate limit, request ID
    auth/                     ← JWT helpers
    model/                    ← structs
  migrations/                 ← SQL up/down
  Dockerfile
```

**Interview e bolo:**  
> “I used a clean layered architecture — handlers stay thin, business logic in services, infrastructure (S3/DB) isolated — so I can test and replace parts independently.”

### 6.1 Request flow (upload) — step by step

1. `main.go` route: `POST /api/v1/images`  
2. Middleware chain: **RateLimit → OptionalAuth → UploadHandler**  
3. Handler: `MaxBytesReader` + `ParseMultipartForm`  
4. Service `ProcessAndUpload`:  
   - read bytes, sniff MIME (jpeg/png/webp)  
   - SHA-256 fingerprint  
   - DB te ready image thakle → return variants (**dedupe**)  
   - else S3 te raw upload → `GenerateVariants` → DB insert → status `ready`  
5. Response JSON: id, fingerprint, urls, variants, deduplicated flag  

**Important files:**
- `internal/handler/image.go` — HTTP  
- `internal/service/image.go` — pipeline  
- `internal/service/processor.go` — thumbnail/small/medium  
- `internal/storage/s3.go` — Put/Get/HeadBucket  

### 6.2 Auth flow

- `POST /api/v1/auth/google` → Google `id_token` verify → user upsert → JWT  
- `POST /api/v1/auth/dev-token` → only non-production  
- Middleware: OptionalAuth (guest ok), RequireAuth (claim)  
- Staging/prod e `JWT_SECRET` + `CORS_ALLOWED_ORIGINS` + SSL required (config validate)

### 6.3 Health

- `/healthz` → process alive  
- `/readyz` → ping DB + Redis + S3 HeadBucket  

K8s liveness/readiness probe ei path use kore.

---

## 7) Frontend code structure

```
apps/object-identifier/src/
  app/page.tsx                 ← upload UI
  app/api/identify/route.ts    ← Gemini server route
  lib/image-api.ts             ← calls Go API (mock fallback)
  components/ResultDisplay.tsx ← AI result UI
```

### Flow

1. User file choose → validate type/size (UI 4MB)  
2. `uploadImage()` → Go API (`NEXT_PUBLIC_IMAGE_API_URL`)  
   - fail hole **mock** + amber warning (graceful degrade)  
3. Same file → `POST /api/identify` → Gemini structured JSON  
4. UI te variants + identification result  

**Keno mock fallback?**  
Demo/interview e backend down thakleo AI flow dekhano jay.

---

## 8) Data & storage design decisions

### Dedupe
- Content-addressed: same bytes → same SHA-256 → reuse record  
- Pros: storage/cost save  
- Cons (know this): currently fingerprint global — privacy/tenancy improve korte ownership-aware dedupe lage  

### Variants on upload (eager)
- Pros: first view fast  
- Cons: CPU on upload path  
- Alternative: lazy generate on first request (future)

### WebP
- Smaller than JPEG/PNG for web delivery  

### Guest TTL 72h
- Temp storage concept; claim kore permanent user ownership  

---

## 9) Security topics (interview gold)

Bolo jei gulo implement:

1. MIME sniff (not only extension)  
2. Size limits (body + business)  
3. Redis rate limit  
4. JWT + Google token verify  
5. CORS allowlist (config-driven)  
6. Secrets not in git (`.env` gitignore, `secret.yaml.example`)  
7. Pre-commit + Gitleaks  
8. CodeQL + GoSec + Semgrep + npm audit  
9. Container non-root, read-only root FS (K8s)  
10. Prod SSL mode require  
11. Variant auth/ownership (JWT / guest session; tenant-scoped dedupe)  
12. Presigned S3 URLs (`AWS_S3_PRESIGN_URLS`)  
13. IRSA / default AWS credential chain (no long-lived keys required in staging/prod)  
14. Structured `http_access` logs + Prometheus `/metrics`  

---

## 10) CI/CD pipeline (presentation slide)

```
Developer commit
    → pre-commit (fmt, lint, secret scan)
    → push GitHub
    → CI (test/build/docker)
    → CodeQL + SAST
    → Staging (build/push image, optional deploy)
    → DAST (ZAP on staging URL)
    → Production (approval + preflight + deploy)
```

Local Windows e `make` na thakle:
```bash
bash scripts/install-hooks.sh
bash scripts/ci-local.sh
bash scripts/smoke-test.sh
```

---

## 11) Local run (demo script)

```bash
# 1 Docker Desktop on
docker compose -f infra/docker/docker-compose.yml up -d

# 2 .env fill (AWS + Postgres defaults)
# 3 API
cd services/image-api && go run ./cmd/server

# 4 UI (.env.local e GEMINI_API_KEY)
cd apps/object-identifier && npm run dev

# 5 Browser http://localhost:3000
# 6 Smoke
bash scripts/smoke-test.sh
```

Readyz must: `"database":"ok","redis":"ok","storage":"ok"`.

---

## 12) API cheatsheet (remember)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/healthz` | no | Liveness |
| GET | `/readyz` | no | Dependencies |
| POST | `/api/v1/images` | optional | Upload |
| GET | `/api/v1/images/{id}/variants/{preset}` | no* | Stream variant |
| POST | `/api/v1/auth/google` | no | Login |
| POST | `/api/v1/auth/dev-token` | no | Dev JWT |
| POST | `/api/v1/users/claim-guest-media` | JWT | Claim guest uploads |
| POST | `/api/identify` (Next) | no | Gemini identify |

\*Ownership on variants = known gap.

---

## 13) Design patterns / SE concepts tumi use korecho

Interview e ei words use koro:

1. **Layered architecture** (handler/service/storage)  
2. **Middleware pipeline** (cross-cutting: CORS, auth, rate limit)  
3. **Content-addressable storage idea** (fingerprint)  
4. **Graceful degradation** (frontend mock if API down)  
5. **12-factor config** (env-based config)  
6. **Health vs readiness** probes  
7. **IaC-ish deploy** (K8s YAML)  
8. **Shift-left security** (pre-commit + SAST in CI)  
9. **Separation of concerns** (image infra API vs AI identify route)  

---

## 14) Sample interview Q&A (Banglish)

**Q: System architecture briefly explain koro.**  
A: Next.js UI upload kore Go API te. API validate + hash + S3 store + WebP variants + Postgres metadata. Redis diye rate limit. Same UI Gemini diye object identify kore alada route e.

**Q: Keno Go backend?**  
A: High-performance I/O, simple deployable binary, good ecosystem for HTTP/S3/image processing; typed and maintainable for infrastructure services.

**Q: Dedupe kivabe?**  
A: Upload bytes er SHA-256. DB te same fingerprint + ready status thakle notun S3 write na kore existing variants return.

**Q: Rate limit kivabe?**  
A: Redis fixed-window counter. Upload 10/min, variant 60/min. Multi-pod e shared state Redis e.

**Q: Secret key frontend e keno nai?**  
A: Gemini key only Next.js server route e (`/api/identify`). Client theke key expose hoy na.

**Q: Production ready koto dur?**  
A: Core pipeline + CI security gates + prod config validation done. Remaining: guest cleanup, stronger tenancy on variants/dedupe, IRSA, metrics.

**Q: Biggest challenge?**  
A: End-to-end local env (Docker, AWS, migrations), Windows tooling (make/gitleaks docker share), SAST false-fail (gosec flags / Next CVE) — debug kore pipeline green korechi.

**Q: Trade-off ekta bolo.**  
A: Eager variant generation — UX better, upload latency/CPU higher. Lazy generation alternative.

---

## 15) Presentation slide order (10–12 min)

1. Problem statement  
2. Demo (upload → variants → identify → dedupe)  
3. Architecture diagram  
4. Stack + why  
5. Upload pipeline deep dive (code walk: handler → service → S3)  
6. Auth + guest model  
7. Security & CI/CD  
8. K8s deploy shape  
9. Known gaps + next steps  
10. Q&A  

---

## 16) File map — “ei feature er code kothay?”

| Topic | File |
|-------|------|
| Server start / routes | `services/image-api/cmd/server/main.go` |
| Upload HTTP | `.../handler/image.go` |
| Dedupe + S3 pipeline | `.../service/image.go` |
| Variant resize | `.../service/processor.go` |
| S3 client | `.../storage/s3.go` |
| Rate limit | `.../middleware/rate_limit.go` |
| Auth middleware | `.../middleware/auth.go` |
| CORS | `.../middleware/cors.go` |
| Config validation | `.../config/config.go` |
| Gemini identify | `apps/object-identifier/src/app/api/identify/route.ts` |
| UI upload | `apps/object-identifier/src/app/page.tsx` |
| Go API client | `apps/object-identifier/src/lib/image-api.ts` |
| Local DB/Redis | `infra/docker/docker-compose.yml` |
| K8s | `deploy/k8s/*` |
| CI/SAST/Deploy | `.github/workflows/*` |

---

## 17) Closing line (confidence)

> “Ei project e ami shudhu CRUD app banai nai — image lifecycle (ingest → fingerprint → transform → store → serve), AI identification, and DevOps security gates eksathe practice korechi. Ami jani ki production-ready, ar ki next harden korte hobe.”

---

*Personal notes — update koro jokhon notun feature add korba.*
