#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "CHANGELOG.md"
  "CONTRIBUTING.md"
  "LICENSE"
  "README.md"
  "SECURITY.md"
  "THIRD_PARTY_NOTICES.md"
  "docs/release.md"
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing required document: $file" >&2
    exit 1
  fi
done

require_text() {
  local file="$1"
  local text="$2"
  if ! grep -Fq "$text" "$file"; then
    echo "$file should mention: $text" >&2
    exit 1
  fi
}

require_text README.md "pure-Go SID engine"
require_text README.md "Rasterklang does not bundle HVSC"
require_text README.md "## Install"
require_text README.md "## CLI Quickstart"
require_text README.md "## Current Limits"
require_text README.md "docs/release.md"

require_text docs/release.md "Release Archives"
require_text docs/release.md "RELEASE_PROVENANCE.json"
require_text docs/release.md "Package Manager Drafts"
require_text docs/release.md "package-channels.yml"

require_text CHANGELOG.md "## Unreleased"
require_text CHANGELOG.md "## v0.1.0"

require_text CONTRIBUTING.md "make check"
require_text CONTRIBUTING.md "Do not commit SID files"

require_text SECURITY.md "Supported Versions"
require_text SECURITY.md "Reporting a Vulnerability"

require_text THIRD_PARTY_NOTICES.md "github.com/ebitengine/oto/v3"
require_text THIRD_PARTY_NOTICES.md "golang.org/x/sys"

echo "Release documents are present."
