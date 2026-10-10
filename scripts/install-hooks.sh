#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

mkdir -p .git/hooks
chmod +x scripts/git-hooks/pre-commit

if command -v pre-commit >/dev/null 2>&1; then
  pre-commit install
  echo "Installed pre-commit framework hooks."
else
  cp scripts/git-hooks/pre-commit .git/hooks/pre-commit
  chmod +x .git/hooks/pre-commit
  echo "Installed fallback .git/hooks/pre-commit (install 'pre-commit' for full hook set)."
fi
