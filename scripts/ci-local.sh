#!/usr/bin/env bash
# Local CI without requiring GNU make (Windows Git Bash friendly).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> gofmt"
(cd services/image-api && test -z "$(gofmt -l .)" || (echo "gofmt needed:" && gofmt -l . && exit 1))

echo "==> go vet + test + build"
(cd services/image-api && go vet ./... && go test ./... -count=1 && go build -o /tmp/image-api-local ./cmd/server)

echo "==> frontend lint"
(cd apps/object-identifier && npm run lint)

echo "ci-local OK"
