# rasterklang Release Notes

This file is for maintainers. The public README should stay focused on what
Rasterklang does and how people use it.

## Release Archives

Release assets are built as `.tar.gz` archives with matching `.sha256` files for:

- Linux amd64 and arm64
- macOS amd64 and arm64
- Windows amd64 and arm64

Verify an archive before testing or publishing it:

```sh
sha256sum -c rasterklang-linux-amd64.tar.gz.sha256
```

On macOS without GNU coreutils:

```sh
shasum -a 256 -c rasterklang-macos-arm64.tar.gz.sha256
```

Release archives are currently unsigned. The checksum is an integrity check, not
a publisher-identity guarantee.

## Provenance

Each CLI archive includes `RELEASE_PROVENANCE.json` with:

- release version
- source commit
- build date
- source repository
- target OS and architecture
- archive name
- dirty-source flag
- available GitHub Actions run context

The provenance file is a machine-readable build record, not a cryptographic
attestation.

## Local Release Gate

Run from a clean `main` checkout:

```sh
make check
```

The gate verifies formatting, generated license reports, package-manifest
contracts, `staticcheck` when installed, `go vet ./...`, and `go test ./...`.
CI installs `staticcheck` before running `make check`. CI and tag-release jobs
also run:

```sh
make race
```

## Tagging

Create and push a release tag:

```sh
make tag VERSION=v0.1.0
make push-tag VERSION=v0.1.0
```

Or do both:

```sh
make release VERSION=v0.1.0
```

Pushing a `v*` tag triggers the release workflow and publishes the matching
GitHub Release assets.

## Identity Preflight

```sh
make identity-preflight
```

This verifies the release checkout points at `dnoegel/rasterklang-cli` and that
`go.mod` declares `github.com/dnoegel/rasterklang-cli`.

## Package Manager Drafts

Release builds generate draft package metadata under `dist/package-manifests/`:

- `homebrew/rasterklang.rb`
- `scoop/rasterklang.json`

Generate them locally with:

```sh
make dist VERSION=v0.1.0
make package-manifests VERSION=v0.1.0
```

The generated Homebrew formula and Scoop manifest are uploaded to the GitHub
Release as reviewable inputs. They do not mean a public tap or bucket has been
published.

The manual Package Channels workflow consumes those release assets:

```sh
gh workflow run package-channels.yml \
  --repo dnoegel/rasterklang-cli \
  -f release_tag=v0.1.0 \
  -f dry_run=true
```

With `PACKAGE_CHANNEL_TOKEN`, `publish_homebrew=true`, and/or
`publish_scoop=true`, it opens package-channel pull requests against
`dnoegel/rasterklang-homebrew-tap` and `dnoegel/rasterklang-scoop-bucket`.

Scoop is the first Windows package path because the current Windows artifacts
are archive-based CLI binaries. `winget` is deferred until Rasterklang publishes
a winget-suitable portable `.exe`/`.zip` or installer artifact.
