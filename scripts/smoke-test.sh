#!/usr/bin/env bash
# Local smoke test for Image API (+ optional Next.js identify).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

API_BASE="${API_BASE:-http://localhost:8080}"
FIXTURE="${FIXTURE:-tests/fixtures/sample.jpg}"
IDENTIFY_URL="${IDENTIFY_URL:-http://localhost:3000/api/identify}"

if [[ ! -f "$FIXTURE" ]]; then
  echo "Missing fixture: $FIXTURE"
  echo "Run: powershell -File tests/fixtures/gen-images.ps1"
  exit 1
fi

tmpdir="tests/fixtures/.smoke-tmp"
rm -rf "$tmpdir"
mkdir -p "$tmpdir"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

# Prefer a Windows-native path for curl multipart on Git Bash.
if command -v cygpath >/dev/null 2>&1; then
  SMOKE_IMG="$(cygpath -w "$ROOT/$FIXTURE")"
else
  SMOKE_IMG="$FIXTURE"
fi

echo "==> healthz"
curl -sf "$API_BASE/healthz" | tee "$tmpdir/healthz.json"
echo

echo "==> readyz"
ready="$(curl -sf "$API_BASE/readyz")"
echo "$ready" | tee "$tmpdir/readyz.json"
echo "$ready" | grep -q '"status":"ready"' || { echo "FAIL: API not ready"; exit 1; }

GUEST_SESSION="smoke-guest-$(date +%s)"

echo "==> upload"
http_code="$(curl -s -D "$tmpdir/upload.headers" -o "$tmpdir/upload.json" -w "%{http_code}" -X POST "$API_BASE/api/v1/images" \
  -H "X-Request-ID: smoke-$(date +%s)" \
  -H "X-Guest-Session-ID: ${GUEST_SESSION}" \
  -F "image=@${SMOKE_IMG};type=image/jpeg")"
cat "$tmpdir/upload.json"
echo
echo "upload HTTP $http_code"
[[ "$http_code" == "200" || "$http_code" == "201" ]] || { echo "FAIL: upload"; exit 1; }

IMG_ID="$(node -e "const fs=require('fs');const j=JSON.parse(fs.readFileSync(process.argv[1],'utf8')); if(!j.success) process.exit(1); process.stdout.write(j.data.id)" "$tmpdir/upload.json")"
echo "image id: $IMG_ID"

echo "==> wait for async variants (worker queue)"
ready_status=""
for _ in $(seq 1 45); do
  curl -sf -H "X-Guest-Session-ID: ${GUEST_SESSION}" "$API_BASE/api/v1/images/${IMG_ID}" > "$tmpdir/image.json" || true
  ready_status="$(node -e "const fs=require('fs');try{const j=JSON.parse(fs.readFileSync(process.argv[1],'utf8'));process.stdout.write(j.success?String(j.data.status||''):'')}catch(e){}" "$tmpdir/image.json")"
  echo "  status=$ready_status"
  if [[ "$ready_status" == "ready" ]]; then
    break
  fi
  if [[ "$ready_status" == "failed" ]]; then
    echo "FAIL: variant worker marked image failed"
    cat "$tmpdir/image.json"
    exit 1
  fi
  sleep 1
done
[[ "$ready_status" == "ready" ]] || { echo "FAIL: timed out waiting for variants"; cat "$tmpdir/image.json"; exit 1; }

echo "==> ownership deny without guest session"
deny_code="$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE/api/v1/images/${IMG_ID}")"
echo "  unauthenticated GET -> HTTP $deny_code"
[[ "$deny_code" == "403" ]] || { echo "FAIL: expected 403 without ownership"; exit 1; }

echo "==> variants"
for preset in thumbnail small medium; do
  out="$tmpdir/${preset}.webp"
  code="$(curl -s -o "$out" -w "%{http_code}" -H "X-Guest-Session-ID: ${GUEST_SESSION}" "$API_BASE/api/v1/images/${IMG_ID}/variants/${preset}")"
  size="$(wc -c < "$out" | tr -d ' ')"
  echo "  $preset -> HTTP $code ($size bytes)"
  [[ "$code" == "200" ]] || { echo "FAIL: variant $preset"; exit 1; }
  [[ "$size" -gt 100 ]] || { echo "FAIL: variant $preset too small"; exit 1; }
done

echo "==> dedupe"
curl -s -o "$tmpdir/dedupe.json" -w "http=%{http_code}\n" -X POST "$API_BASE/api/v1/images" \
  -H "X-Guest-Session-ID: ${GUEST_SESSION}" \
  -F "image=@${SMOKE_IMG};type=image/jpeg"
cat "$tmpdir/dedupe.json"
echo
grep -q '"deduplicated":true' "$tmpdir/dedupe.json" || { echo "FAIL: expected deduplicated=true"; exit 1; }

echo "==> metrics"
curl -sf "$API_BASE/metrics" | tee "$tmpdir/metrics.txt" | grep -q "image_api_http_requests_total" || { echo "FAIL: metrics missing"; exit 1; }

code="$(curl -s -o /dev/null -w "%{http_code}" "$IDENTIFY_URL" || true)"
if [[ "$code" != "000" ]]; then
  echo "==> identify (Next.js)"
  curl -s -o "$tmpdir/identify.json" -X POST "$IDENTIFY_URL" -F "image=@${SMOKE_IMG};type=image/jpeg" || true
  cat "$tmpdir/identify.json"
  echo
  if grep -q '"success":true' "$tmpdir/identify.json" 2>/dev/null; then
    echo "identify OK"
  elif grep -q 'MISSING_API_KEY' "$tmpdir/identify.json" 2>/dev/null; then
    echo "WARN: GEMINI_API_KEY not set in apps/object-identifier/.env.local"
  else
    echo "WARN: identify did not succeed (frontend may be down or Gemini error)"
  fi
else
  echo "==> identify skipped (Next.js not reachable on :3000)"
fi

echo
echo "All Image API smoke checks passed."
