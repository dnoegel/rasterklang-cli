#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "CHANGELOG.md"
  "CONTRIBUTING.md"
  "LICENSE"
  "README.md"
  "SECURITY.md"
  "THIRD_PARTY_NOTICES.md"
)

for file in "${required_files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "missing required release document: $file" >&2
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

require_text README.md "Verify checksums"
require_text README.md "Release Identity Preflight"
require_text README.md "dnoegel/rasterklang-cli"
require_text README.md "github.com/dnoegel/rasterklang-cli"
require_text README.md "sha256sum -c"
require_text README.md "shasum -a 256 -c"
require_text README.md "Signing status"
require_text README.md "Release provenance"
require_text README.md "RELEASE_PROVENANCE.json"
require_text README.md "Package managers"
require_text README.md "make package-manifests"
require_text README.md "Homebrew formula"
require_text README.md "Scoop manifest"
require_text README.md "Scoop is the first Windows package path"
require_text README.md "winget is deferred"
require_text README.md "dist/package-manifests"
require_text README.md "Package Channels workflow"
require_text README.md "package-channels.yml"
require_text README.md "PACKAGE_CHANNEL_TOKEN"
require_text README.md "rasterklang-homebrew-tap"
require_text README.md "rasterklang-scoop-bucket"
require_text README.md "Rasterklang does not bundle HVSC"
require_text README.md "## Installation"
require_text README.md "## Support Matrix"
require_text README.md "## Unsupported Tune Behavior"
require_text README.md "## Troubleshooting"
require_text README.md "Linux audio backends"
require_text README.md "Unsupported RSID/BASIC/ROM edge cases"
require_text README.md "BASIC/KERNAL ROM"
require_text README.md "C64 ROM images"
require_text README.md "No package manager formula is published yet"
require_text README.md "Release quality gate"
require_text README.md "staticcheck"
require_text README.md "make race"

require_text CHANGELOG.md "## Unreleased"
require_text CHANGELOG.md "## v0.1.0"

require_text CONTRIBUTING.md "make check"
require_text CONTRIBUTING.md "staticcheck"
require_text CONTRIBUTING.md "make race"
require_text CONTRIBUTING.md "Ignored Local Artifacts"
require_text CONTRIBUTING.md "hvsc-compat-failures.tsv"
require_text CONTRIBUTING.md "docs/labeling-feasibility.md"
require_text CONTRIBUTING.md "docs/superpowers/"
require_text CONTRIBUTING.md "docs/sid-engine-notes.md"
require_text CONTRIBUTING.md "Do not commit SID files"

require_text SECURITY.md "Supported Versions"
require_text SECURITY.md "Reporting a Vulnerability"

require_text THIRD_PARTY_NOTICES.md "github.com/ebitengine/oto/v3"
require_text THIRD_PARTY_NOTICES.md "Apache-2.0"
require_text THIRD_PARTY_NOTICES.md "golang.org/x/sys"
require_text THIRD_PARTY_NOTICES.md "BSD-3-Clause"
