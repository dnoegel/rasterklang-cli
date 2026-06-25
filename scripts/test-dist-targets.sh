#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION="${TEST_DIST_VERSION:-v0.0.0-dist-test}"
COMMIT="${TEST_DIST_COMMIT:-disttest}"
DATE="${TEST_DIST_DATE:-2026-06-24T18:00:00Z}"

make dist VERSION="$VERSION" COMMIT="$COMMIT" DATE="$DATE" >/dev/null

require_file() {
  if [[ ! -f "$1" ]]; then
    echo "missing expected release file: $1" >&2
    exit 1
  fi
}

require_archive_entry() {
  local archive="$1"
  local entry="$2"
  if ! tar -tzf "$archive" | grep -qx "$entry"; then
    echo "archive $archive is missing expected entry: $entry" >&2
    exit 1
  fi
}

require_checksum() {
  local checksum="$1"
  local archive_name="$2"
  if ! grep -Eq "^[a-f0-9]{64}[[:space:]]+$archive_name$" "$checksum"; then
    echo "checksum file $checksum does not reference $archive_name" >&2
    exit 1
  fi
}

for target in \
  linux-amd64:rasterklang-linux-amd64 \
  linux-arm64:rasterklang-linux-arm64 \
  macos-amd64:rasterklang-macos-amd64 \
  macos-arm64:rasterklang-macos-arm64 \
  windows-amd64:rasterklang-windows-amd64.exe \
  windows-arm64:rasterklang-windows-arm64.exe
do
  label="${target%%:*}"
  binary="${target#*:}"
  archive="dist/rasterklang-${label}.tar.gz"
  checksum="${archive}.sha256"

  require_file "$archive"
  require_file "$checksum"
  require_archive_entry "$archive" "rasterklang-${label}/$binary"
  require_archive_entry "$archive" "rasterklang-${label}/LICENSE"
  require_archive_entry "$archive" "rasterklang-${label}/THIRD_PARTY_NOTICES.md"
  require_archive_entry "$archive" "rasterklang-${label}/THIRD_PARTY_LICENSE_REPORT.md"
  require_archive_entry "$archive" "rasterklang-${label}/RELEASE_PROVENANCE.json"
  require_checksum "$checksum" "rasterklang-${label}.tar.gz"
done

require_file "dist/package-manifests/homebrew/rasterklang.rb"
require_file "dist/package-manifests/scoop/rasterklang.json"

if ! grep -Fq "rasterklang-macos-arm64.tar.gz" "dist/package-manifests/homebrew/rasterklang.rb"; then
  echo "Homebrew formula does not reference macOS release archives" >&2
  exit 1
fi

if ! grep -Fq "rasterklang-windows-amd64.tar.gz" "dist/package-manifests/scoop/rasterklang.json"; then
  echo "Scoop manifest does not reference Windows release archives" >&2
  exit 1
fi
