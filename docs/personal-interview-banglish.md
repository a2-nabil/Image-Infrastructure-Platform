# Image Infrastructure Platform — Personal A2Z Guide (Banglish)

> Ei file amar personal notes — interview / presentation e explain korar jonno.  
> Language: Banglish (Bangla + English mix). Focus: **SRS, stack, code, keno ki use korlam**.

---

## 1) Project one-liner (elevator pitch)

Ami ekta **full-stack Image Infrastructure Platform** banaisi jekhane:

1. User image upload kore (Converter / Identify)  
2. Backend image **store** kore (S3), **tenant-scoped dedupe** kore (SHA-256), **variants** banay (WebP)  
3. Frontend Gemini diye image er **main object identify** kore  
4. Personal **Gallery** (Google Photos–style) — only **super admin + allowlist**  

Interview e bolte paro:

> “I built an image platform with a Go REST API for upload/processing/storage, Postgres for metadata, Redis for rate limiting, S3 for objects, and a Next.js app for Converter, Identify, Dashboard, and an allowlisted personal Gallery — with ownership enforcement, library_scope isolation, Docker locally, and Kubernetes + GitHub Actions for deployment/security gates.”

---

## 2) Problem / keno ei project?

| Problem | Solution ami implement korlam |
|---------|--------------------------------|
| Same image bar bar upload → storage waste | SHA-256 fingerprint + **tenant + library_scope** dedupe |
| Mobile/web e different sizes lage | On-upload / async **variant generation** |
| Guest user o upload korte chay | Guest session + 72h TTL + claim (session-bound) |
| Abuse / spam upload | Redis **rate limiting** (tools + gallery chunk limits) |
| “Ei image e ki ache?” | Gemini vision (Next `/api/identify` + API identify store) |
| Private photos vs tools history | `library_scope=tools` vs `gallery` — Dashboard ≠ Gallery |
| Gallery who can see? | Super admin email + `gallery_access_emails` + `RequireGalleryAccess` |
| Prod e blind deploy | CI → CodeQL → SAST → Staging → DAST → Production |

---

## 3) SRS style — requirements

### 3.1 Functional

1. JPEG/PNG/WebP upload (`POST /api/v1/images`) — tools library  
2. MIME + size validate; rate limit  
3. Tenant-scoped dedupe (`deduplicated: true`)  
4. WebP variants: thumbnail / small / medium  
5. Ownership on original + variants (JWT / guest / capability URL)  
6. Google auth + JWT; guest upload possible  
7. Guest claim must match bound guest session (no spoof)  
8. Identify: object name, confidence, summary  
9. Dashboard: signed-in **tools** list only  
10. Gallery: chunked upload, albums, trash — **allowlist only**  
11. Health: `/healthz`, `/readyz`

### 3.2 Non-functional

| Area | Ami ki korechi |
|------|----------------|
| Security | JWT/Google, gallery allowlist, tenant capability URLs, CORS, SSL require in prod |
| Scalability | K8s Deployment + HPA |
| Reliability | Graceful shutdown, guest/trash sweepers |
| Observability | `X-Request-ID`, `/metrics` |
| Deploy | Docker Compose local; K8s; GH Actions |

### 3.3 Honest gaps / backlog

- Full Prometheus/OTel dashboards  
- Broader multi-tenant “public photos product” (Gallery is intentionally private allowlist)  

> “MVP e core pipeline + gallery isolation + CI security gates done; remaining hardening backlog e.”

---

## 4) High-level architecture

```
Browser (Next.js :3000)
   │
   ├─► Go Image API :8080  ──► PostgreSQL (metadata, users, gallery_access, albums)
   │         │               ──► Redis (rate limit, queues)
   │         └───────────────► AWS S3 (tools + users/{id}/gallery/...)
   │
   └─► Next.js /api/identify ──► Google Gemini
```

| Path | Role |
|------|------|
| `apps/web/` | Next.js frontend |
| `services/api/` | Go backend API |
| `infra/docker/` | Local Postgres + Redis |
| `deploy/k8s/` | Kubernetes manifests |
| `docs/security.md` | Ownership + gallery security |
| `.github/workflows/` | CI/CD + security pipelines |

---

## 5) Tech stack — ki + **KENO**

- **Go** — HTTP/S3/image pipeline, single binary  
- **Next.js 16** — App Router; Gemini key server-side only  
- **Postgres** — images, variants, users, albums, `gallery_access_emails`, `library_scope`  
- **Redis** — rate limits + workers  
- **S3** — tools vs `users/{id}/gallery/...`  
- **Gemini** — vision identify  
- **Docker / K8s / Actions** — local + promote path with SAST/DAST  

---

## 6) Backend structure

```
services/api/
  cmd/server/main.go
  internal/handler/     image, user, gallery, gallery_access, identify, auth
  internal/service/     image, album, gallery_upload, gallery_access, access_token
  internal/middleware/  auth (RequireGalleryAccess), CORS, rate limit
  migrations/           …000009 gallery_access, 000010 library_scope
```

**Upload flow:** RateLimit → OptionalAuth → validate → fingerprint → tenant/scope dedupe → S3 → variants → DB.

**Gallery upload:** RequireGalleryAccess → init → concurrent chunks (atomic `part_etags` merge) → complete.

**Auth:** Google ID token → JWT; `RequireAuth` / `RequireGalleryAccess`; staging/prod JWT + CORS + SSL validation.

---

## 7) Frontend structure

```
apps/web/src/
  app/converter|identify|dashboard|gallery/
  components/SiteHeader, PhotosSheet, GalleryUploadHost, ThemeProvider, AuthProvider
  lib/gallery-api, gallery-upload-queue, gallery-cache, user-api, auth
```

Gallery: ⋮ menu, full-page drop, durable upload queue across routes.  
Dashboard ≠ Gallery. Soft-revalidate never keeps another user’s grid when `total=0`.

---

## 8) Security topics (interview gold)

1. MIME sniff + size limits  
2. Redis rate limit (tools + gallery)  
3. JWT + Google verify; prod config gates  
4. **Gallery allowlist** + server `403 GALLERY_FORBIDDEN`  
5. **library_scope** isolation (tools vs gallery)  
6. Tenant-bound capability URLs (~1h)  
7. Guest claim session binding  
8. Soft-revalidate / cache keyed by user  
9. Secrets not in git; gitleaks + CodeQL + GoSec + Semgrep + npm audit + ZAP  
10. Non-root container; IRSA preferred over long-lived keys  

Details: [docs/security.md](security.md).

---

## 9) CI/CD

```
pre-commit → CI → CodeQL + SAST → Staging → DAST → Production (approval)
```

Local: `bash scripts/ci-local.sh`, `bash scripts/smoke-test.sh`.

---

## 10) Local demo

```bash
docker compose -f infra/docker/docker-compose.yml up -d
cp .env.example .env   # JWT_SECRET, GOOGLE_CLIENT_ID, GALLERY_SUPER_ADMIN_EMAIL
cd services/api && go run ./cmd/server
cd apps/web && cp .env.local.example .env.local && npm run dev
# http://localhost:3000
```

---

## 11) API cheatsheet

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | `/healthz` `/readyz` | no | Health |
| POST | `/api/v1/images` | optional | Tools upload |
| GET | `/api/v1/users/me/images` | JWT | Tools library |
| POST | `/api/v1/users/claim-guest-media` | JWT | Claim guest (session-bound) |
| GET | `/api/v1/users/me/gallery/media` | Gallery | Gallery list |
| POST/PUT | `/api/v1/users/me/uploads*` | Gallery | Chunked upload |
| * | `/api/v1/users/me/albums*` | Gallery | Albums |
| * | `/api/v1/admin/gallery-access*` | Super admin | Allowlist |

Full contract: [docs/api/v1-contract.md](api/v1-contract.md).

---

## 12) Sample Q&A

**Q: Dashboard ar Gallery same?**  
A: Na. Dashboard = `library_scope=tools`. Gallery = `gallery` + allowlist. Separate APIs and S3 prefixes.

**Q: Other signed-in user Gallery dekhte parbe?**  
A: UI hide + API `RequireGalleryAccess`. Allowlist e na thakle `403`. Super admin email allowlist manage kore.

**Q: Cross-account photo leak kivabe block?**  
A: Ownership on every media route; capability URL tenant-bound; claim guest id must match session; caches keyed by user; soft-revalidate respects `total`.

**Q: Secret key frontend e keno nai?**  
A: Gemini only Next server route; JWT secret + AWS only API env.

---

## 13) Closing

> “Ei project e ami image lifecycle (ingest → fingerprint → transform → store → serve), AI identify, personal Gallery isolation, and DevOps security gates eksathe practice korechi — ar jani UI hide alone security na, API enforce korte hoy.”

*Personal notes — update koro jokhon notun feature add korba.*
