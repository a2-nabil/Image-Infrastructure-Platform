#!/usr/bin/env bash
# Ensure a native gitleaks binary exists (avoids Docker Desktop file-sharing issues
# on Windows paths that contain spaces).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${GITLEAKS_VERSION:-8.24.2}"
TOOLS_DIR="${ROOT}/.tools"
BIN="${TOOLS_DIR}/gitleaks"

if command -v gitleaks >/dev/null 2>&1; then
  command -v gitleaks
  exit 0
fi

if [[ -x "$BIN" ]] || [[ -x "${BIN}.exe" ]]; then
  if [[ -x "${BIN}.exe" ]]; then
    echo "${BIN}.exe"
  else
    echo "$BIN"
  fi
  exit 0
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="x64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "unsupported arch: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  mingw*|msys*|cygwin*|windows*)
    asset="gitleaks_${VERSION}_windows_${arch}.zip"
    out="${BIN}.exe"
    ;;
  linux*)
    asset="gitleaks_${VERSION}_linux_${arch}.tar.gz"
    out="$BIN"
    ;;
  darwin*)
    asset="gitleaks_${VERSION}_darwin_${arch}.tar.gz"
    out="$BIN"
    ;;
  *)
    echo "unsupported os: $os" >&2
    exit 1
    ;;
esac

url="https://github.com/gitleaks/gitleaks/releases/download/v${VERSION}/${asset}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

mkdir -p "$TOOLS_DIR"
echo "[ensure-gitleaks] downloading ${asset}..." >&2
curl -fsSL "$url" -o "${tmpdir}/${asset}"

if [[ "$asset" == *.zip ]]; then
  if command -v unzip >/dev/null 2>&1; then
    unzip -qo "${tmpdir}/${asset}" -d "$tmpdir"
  else
    powershell.exe -NoProfile -Command "Expand-Archive -LiteralPath '${tmpdir}/${asset}' -DestinationPath '${tmpdir}' -Force" >/dev/null
  fi
  src="$(find "$tmpdir" -maxdepth 2 -type f \( -name 'gitleaks.exe' -o -name 'gitleaks' \) | head -n 1)"
else
  tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"
  src="$(find "$tmpdir" -maxdepth 2 -type f -name 'gitleaks' | head -n 1)"
fi

if [[ -z "${src:-}" ]]; then
  echo "[ensure-gitleaks] failed to extract gitleaks binary" >&2
  exit 1
fi

cp "$src" "$out"
chmod +x "$out"
echo "$out"
