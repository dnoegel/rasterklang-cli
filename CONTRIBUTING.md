# Contributing

Rasterklang is a pure-Go SID engine and CLI. Keep changes small, documented, and
covered by the closest practical test.

## Local Checks

Run the full release gate before sending changes:

```sh
make check
```

`make check` runs format checks, release-document guards, generated license
report checks, `go vet`, Go tests, and `staticcheck` when it is installed.
GitHub Actions installs `staticcheck` before running `make check`, so CI treats
static analysis as a required release gate. CI and tag-release jobs also run the
race detector through:

```sh
make race
```

For release archive changes, also run:

```sh
make test-dist-targets
```

## Engine Changes

When changing SID playback, timing, emulation behavior, audio quality, or sound
profile constants, update `docs/sid-engine-notes.md` in the same change. Explain
what changed, why it changed, what tune or audible problem motivated it, and
which uncertainty remains.

## Legal Data Boundaries

Do not commit SID files, HVSC extracts, C64 ROM images, generated compatibility
reports from private corpora, or other third-party media unless the license is
explicitly documented and compatible with redistribution.

Small synthetic fixtures created for tests are fine when they are source-authored
in the test and do not copy third-party tune data.

## Ignored Local Artifacts

The repository intentionally ignores local reports and agent working material
that can be useful during compatibility research but should not ship in source
or release archives:

- `hvsc-compat-failures.tsv`
- `docs/labeling-feasibility.md`
- `docs/superpowers/`
- `dist/`

Before publishing a release, confirm that any information from these files that
belongs in public documentation has been summarized in tracked docs such as
`docs/hvsc-compat-findings.md` or `docs/sid-engine-notes.md`.

## Release Changes

Release-facing edits should keep these files current:

- `README.md` for installation and current limitations.
- `CHANGELOG.md` for user-visible changes.
- `THIRD_PARTY_NOTICES.md` for dependency and artifact notices.
- `.github/workflows/release.yml` for tag-built artifacts.
- `scripts/test-dist-targets.sh` for archive contract changes.
