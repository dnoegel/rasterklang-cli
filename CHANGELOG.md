# Changelog

All notable changes to Rasterklang should be recorded here before a public tag is
created. The project follows semantic version-style tags such as `v0.1.0`.

## Unreleased

- SID envelopes now use a 15-bit rate counter that reproduces the ADSR delay
  bug, and sustain keeps the decay period.
- SID noise is clocked on accumulator bit 19 (16 times per period, previously
  once) and is reset by the test bit.
- 6502 read-modify-write instructions perform the dummy write of the original
  value, and reads of write-only SID registers return the fading bus value.
- Added MIT licensing and starter third-party notices.
- Added CLI version metadata through `rasterklang --version` and
  `rasterklang version`.
- Added Linux, macOS, and Windows release archive targets with SHA-256 checksum
  files.
- Added `make check` as the local and CI release gate for formatting, vetting,
  release documentation, and tests.
- Added release documentation for checksum verification, unsigned binary status,
  package-manager status, and HVSC/tune licensing.
- Replaced remaining public "POC" support wording with release-grade
  Rasterklang support labels and errors.

## v0.1.0

Initial public release target. This version is not tagged until the release
checklist in `Rasterklang-Releaseplan.md` is complete for the core CLI.
