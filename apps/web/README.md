# Web (Next.js)

Frontend for the Image Infrastructure Platform: Converter, Identify, Dashboard (tools library), and personal Gallery (allowlist).

## Stack

- Next.js 16 (App Router) + React 19 + TypeScript
- Tailwind CSS (light / dark via `ThemeProvider`, `html.dark`, `a2n-theme`)
- Google Identity Services → API JWT
- Proxies `/api/v1/*` to the Go API (`IMAGE_API_ORIGIN`)

## Routes

| Path | Notes |
|------|--------|
| `/` | Landing |
| `/converter` | Format conversion (guest or signed-in) |
| `/identify` | Gemini identification UI |
| `/dashboard` | Signed-in **tools** library only |
| `/gallery` | Personal Gallery — **allowlist only** (super admin + granted emails) |
| `/login` | Google sign-in |

Gallery is **not** in the primary nav. Super admin / allowlisted users open it from the header ⋮ menu. Profile / brand links go to **Dashboard**, not Gallery.

## Gallery UX

- Google Photos–style grid, albums, trash, select mode
- Actions (upload, album, trash, access, theme) in the ⋮ menu
- Full-page drag-and-drop upload
- Process-wide upload queue (`gallery-upload-queue` + `GalleryUploadHost` in root layout) so uploads survive route changes
- Chunked multipart upload with XHR byte progress; concurrent chunks; completion retry
- Overlays: `PhotosSheet` (bottom sheet &lt; `md`, centered modal `md+`) — see workspace UI rules

## Security (frontend)

- Gallery links and Access UI only when `hasGalleryAccess` (email === super admin or server allowlist).
- All gallery mutations go through authenticated API helpers; **backend** returns `403 GALLERY_FORBIDDEN` if not allowed — never rely on UI alone.
- Session / gallery caches keyed by user + library kind (`tools` \| `gallery`); cleared on logout / account switch.
- Soft-revalidate does not keep a previous user’s rows when the new list `total` is 0.
- Guest session id is rotated after successful claim; claim body must match bound guest header (enforced by API).

See [docs/security.md](../../docs/security.md).

## Local setup

```bash
cp .env.local.example .env.local
# Set NEXT_PUBLIC_GOOGLE_CLIENT_ID, IMAGE_API_ORIGIN, GEMINI_API_KEY
npm install
npm run dev
```

| Variable | Purpose |
|----------|---------|
| `NEXT_PUBLIC_IMAGE_API_URL` | Usually `/api/v1` (same-origin proxy) |
| `IMAGE_API_ORIGIN` | Go API origin (e.g. `http://localhost:8080`) |
| `NEXT_PUBLIC_GOOGLE_CLIENT_ID` | Google OAuth web client (public) |
| `GEMINI_API_KEY` | Server-only; used by `/api/identify` |

Never put JWT secrets, AWS keys, or Gemini keys in `NEXT_PUBLIC_*` — Gemini stays on the Next server route; JWT/AWS stay on the API / root `.env`.

## Scripts

```bash
npm run dev
npm run build
npm run start
npm run lint
```

## Related

- Root [README](../../README.md)
- [API contract](../../docs/api/v1-contract.md)
- [Security](../../docs/security.md)
