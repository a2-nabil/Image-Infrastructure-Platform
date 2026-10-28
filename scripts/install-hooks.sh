#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p .git/hooks
chmod +x scripts/git-hooks/pre-commit scripts/ensure-gitleaks.sh

# Cache a native gitleaks binary so Windows Docker file-sharing is not required.
if bash scripts/ensure-gitleaks.sh >/dev/null; then
  echo "gitleaks ready: $(bash scripts/ensure-gitleaks.sh)"
else
  echo "WARN: could not install native gitleaks; hook may fall back to Docker."
fi

if command -v pre-commit >/dev/null 2>&1; then
  pre-commit install
  echo "Installed pre-commit framework hooks."
else
  cp scripts/git-hooks/pre-commit .git/hooks/pre-commit
  chmod +x .git/hooks/pre-commit
  echo "Installed fallback .git/hooks/pre-commit (install 'pre-commit' for full hook set)."
fi
