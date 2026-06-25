# rasterklang

[![Build binaries](https://github.com/dnoegel/rasterklang-cli/actions/workflows/binaries.yml/badge.svg)](https://github.com/dnoegel/rasterklang-cli/actions/workflows/binaries.yml)
[![Release](https://github.com/dnoegel/rasterklang-cli/actions/workflows/release.yml/badge.svg)](https://github.com/dnoegel/rasterklang-cli/actions/workflows/release.yml)

Pure-Go SID engine and CLI for PSID/RSID tunes.

This is a from-scratch implementation, not a libsidplayfp wrapper. The project
currently provides:

- PSID/RSID metadata parsing, including filterable tune type labels such as
  `RSID`, `BASIC`, `Magic Voice`, `SAM/Reciter`, `Electronic Speech Systems`,
  and `Sound Master`
- HVSC compatibility scanning with type filters such as
  `-exclude-type "Electronic Speech Systems"`
- a MOS 6502/6510 CPU core with many common undocumented opcodes
- a 64K C64 memory bus with SID register mapping
- direct `play` address and minimal IRQ-driven tune playback
- a model-aware three-voice SID path with oversampling
- ADSR, D418 sample, OSC3, ENV3, model-aware multimode filter, and output filtering
- offline WAV rendering and pull-based sample streaming for apps

## Installation

Download the latest release for your platform from
<https://github.com/dnoegel/rasterklang-cli/releases/latest>:

- `rasterklang-linux-amd64`
- `rasterklang-linux-arm64`
- `rasterklang-macos-amd64`
- `rasterklang-macos-arm64`
- `rasterklang-windows-amd64`
- `rasterklang-windows-arm64`

Each release asset includes a `.tar.gz` archive and a `.sha256` checksum. Unpack
the archive and put the binary somewhere on your `PATH`:

```sh
tar -xzf rasterklang-linux-amd64.tar.gz
./rasterklang-linux-amd64 --version
./rasterklang-linux-amd64 play path/to/tune.sid
```

### Verify checksums

Download the matching `.sha256` file next to the archive and verify it before
running the binary:

```sh
sha256sum -c rasterklang-linux-amd64.tar.gz.sha256
```

On macOS:

```sh
shasum -a 256 -c rasterklang-macos-arm64.tar.gz.sha256
```

### Signing status

Current release archives are unsigned. Treat the published `.sha256` files as an
integrity check, not as an authenticity or publisher-identity guarantee. Binary
signing, SLSA provenance, and package-manager attestations are deferred until the
first public release process is finalized.

### Release provenance

Each CLI archive includes `RELEASE_PROVENANCE.json`. It records the release
version, source commit, build date, source repository, target OS/architecture,
archive name, dirty-source flag, and available GitHub Actions run context.

This provenance file is a machine-readable build record, not a cryptographic
attestation. Use it together with the `.sha256` file and the GitHub Release tag;
signed attestations remain deferred.

### Package managers

Package managers are not published yet. Release builds now generate draft
package metadata under `dist/package-manifests/`:

- `homebrew/rasterklang.rb` for a future Homebrew tap
- `scoop/rasterklang.json` for the first Windows package path

Generate those files from checked release archives and checksums with:

```sh
make dist VERSION=v0.1.0
make package-manifests VERSION=v0.1.0
```

The generated Homebrew formula and Scoop manifest are uploaded to the GitHub
Release as reviewable package-channel inputs, but they do not mean a public tap
or bucket has already been published. Scoop is the first Windows package path
because the current Windows artifacts are archive-based CLI binaries.
`winget` is deferred until the release publishes a winget-suitable portable
`.exe`/`.zip` or installer artifact. Until a Homebrew tap, Scoop bucket, and
Linux package path are published, install Rasterklang from the GitHub release
archives or with `go install`.

The manual Package Channels workflow consumes those release assets after a tag
is published:

```sh
gh workflow run package-channels.yml \
  --repo dnoegel/rasterklang-cli \
  -f release_tag=v0.1.0 \
  -f dry_run=true
```

The workflow downloads `dist/package-manifests/homebrew/rasterklang.rb` and
`dist/package-manifests/scoop/rasterklang.json` from the GitHub Release. In
dry-run mode it validates the formula/manifest only. With
`PACKAGE_CHANNEL_TOKEN`, `publish_homebrew=true`, and/or `publish_scoop=true`,
it opens package-channel pull requests against
`dnoegel/rasterklang-homebrew-tap` and `dnoegel/rasterklang-scoop-bucket`.

Rasterklang does not bundle HVSC, C64 ROM images, or third-party SID files. Use
your own legally obtained `.sid` collection with the CLI.

Windows archives contain `.exe` binaries, for example
`rasterklang-windows-amd64.exe`.

### Go install

If you have Go installed, you can also install the CLI from the module:

```sh
go install github.com/dnoegel/rasterklang-cli/cmd/rasterklang@latest
```

### Development checkout

For development from a checkout:

```sh
go run ./cmd/rasterklang play path/to/tune.sid
go run ./cmd/rasterklang info path/to/tune.sid
go run ./cmd/rasterklang duration path/to/tune.sid
go run ./cmd/rasterklang duration-validate -songlengths ~/C64Music/DOCUMENTS/Songlengths.md5 path/to/tunes
go run ./cmd/rasterklang render -duration 30s -o tune.wav path/to/tune.sid
go run ./cmd/rasterklang analyze -duration 30s path/to/tune.sid
```

## CLI

## Releases

Create releases from a clean `main` checkout:

```sh
make check
make tag VERSION=v0.1.0
make push-tag VERSION=v0.1.0
```

Or create and push the tag in one step:

```sh
make release VERSION=v0.1.0
```

### Release quality gate

`make check` is the local release gate. It verifies formatting, release
documentation, generated license-report and package-manifest contracts,
`staticcheck` when installed, `go vet ./...`, and `go test ./...`. GitHub
Actions installs `staticcheck` before running `make check`, so CI and tag
release builds treat static analysis as required. CI and tag-release jobs also
run `make race`, which executes `go test -race ./...` before any release archive
is published.

Pushing a `v*` tag triggers the release workflow, which builds Linux, macOS,
and Windows archives with embedded version metadata and publishes them to the
matching GitHub Release. The same release job also generates draft
`dist/package-manifests/homebrew/rasterklang.rb` and
`dist/package-manifests/scoop/rasterklang.json` files from the release checksums
and uploads them as package-channel inputs. The Scoop manifest is the checked
Windows package draft for the first release; winget is deferred until the
Windows artifact shape is changed for that channel.

### Release Identity Preflight

```sh
make identity-preflight
```

This verifies the release checkout is pointed at the public
`dnoegel/rasterklang-cli` repository and that `go.mod` declares
`github.com/dnoegel/rasterklang-cli`. Fix the origin remote, module path, README
links, and release workflow URLs before tagging if this preflight fails.

### `version`

Prints the release version, source commit, and build timestamp:

```sh
rasterklang --version
rasterklang version
```

### `info`

Prints SID metadata, filterable tune type labels, and the engine's current
support verdict:

```sh
rasterklang info Commando.sid
```

### `play`

Streams a tune to your audio device:

```sh
rasterklang play -subtune 2 -duration 3m Commando.sid
```

Useful flags:

```sh
-subtune 2      # 1-based subtune number; defaults to SID default subtune
-duration 3m    # selected playback span; use 0 to play until interrupted
-start 30s      # skip into the tune before playback
-rate 48000     # output sample rate
-profile path   # sound profile name or JSON path; defaults to balanced
-volume 0.8     # playback gain multiplier
-fade-in 5ms    # smooth the start of each play span
-fade-out 25ms  # smooth the end of each finite play span
-loop           # repeat the selected span until Ctrl-C
-quiet          # suppress status output
```

By default `play` estimates the selected subtune duration with a bounded
heuristic pass, streams audio, applies short fades to avoid edge clicks, and
shows elapsed progress on interactive terminals. If no confident duration is
found, it falls back to a 3 minute play span. Pass `-duration` to override this.

On macOS, playback uses the Go audio backend directly. On Linux, the default
build streams raw PCM to common system players (`aplay`, `ffplay`, `paplay`,
then `pw-play`) so the CLI still builds on machines without ALSA development
headers. For direct ALSA output through the Go audio backend:

```sh
go build -tags rasterklang_alsa ./cmd/rasterklang
```

### `render`

Writes mono 16-bit WAV:

```sh
rasterklang render -subtune 1 -duration 2m -rate 44100 -o tune.wav Commando.sid
rasterklang render -profile profile-candidate.json -duration 30s -o candidate.wav Commando.sid
```

### `analyze`

Reports peak, RMS, DC offset, maximum sample delta, clipped samples, crest
factor, and zero crossings. It accepts SID files or mono 16-bit WAV files:

```sh
rasterklang analyze -duration 30s Commando.sid
rasterklang analyze tune.wav
```

### `duration`

Estimates SID playback length without a songlength database:

```sh
rasterklang duration Commando.sid
rasterklang duration -all Commando.sid
rasterklang duration -budget 3s -max 8m Commando.sid
```

The heuristic renders quickly at a low sample rate, watches SID register/audio
fingerprints for repeated windows, and also detects trailing silence. Results
include the source, confidence, simulated span, and reason. This is a fallback,
not a replacement for HVSC `Songlengths` data.

To compare the heuristic against an HVSC songlength database:

```sh
rasterklang duration-validate \
  -songlengths ~/C64Music/DOCUMENTS/Songlengths.md5 \
  -threshold 5s \
  ~/C64Music/MUSICIANS/H/Hubbard_Rob
```

By default the validator prints mismatches, unknown estimates, missing database
entries, and errors. Add `-show-ok` to print every compared subtune. The current
parser supports the modern HVSC `Songlengths.md5` format, where entries are
looked up by MD5 over the full SID file content including the header.

## Go Library

Import the root package:

```go
import sid "github.com/dnoegel/rasterklang-cli"
```

The public API intentionally keeps emulator internals under `internal/`. Apps
should use the root package for parsing, metadata, offline rendering, streaming,
WAV helpers, and audio analysis.

### Load Metadata

```go
tune, err := sid.LoadFile("Commando.sid")
if err != nil {
	return err
}

fmt.Println(tune.Title, tune.Author, tune.Songs)
```

### Render a Fixed Duration

Use `Render` when you want a complete buffer, for example to write a WAV, run an
analysis pass, create previews, or export audio:

```go
pcm, err := sid.Render(tune, sid.RenderOptions{
	Subtune:    1,
	Duration:   30 * time.Second,
	SampleRate: 44100,
})
if err != nil {
	return err
}

return sid.WriteWAV("preview.wav", 44100, pcm)
```

### Sound Profiles

The default sound is the built-in `balanced` profile. For how the current
filter profile and BASIC timing defaults were calibrated, see
[docs/filter-and-timings.md](docs/filter-and-timings.md). Callers can pass JSON
profile candidates into the renderer without changing Go constants:

```go
soundProfile, err := sid.LoadSoundProfile("profile-candidate.json")
if err != nil {
	return err
}

pcm, err := sid.Render(tune, sid.RenderOptions{
	Duration:     30 * time.Second,
	SampleRate:   44100,
	SoundProfile: &soundProfile,
})
```

Profiles can currently override mixer, waveform color, filter, cutoff, and
output-stage parameters. `filter.cutoff.points` can optionally provide a
piecewise cutoff-Hz curve; when present it overrides the polynomial
base/range/exponent cutoff mapping. Candidate profiles should be treated as
experimental until validated against sweeps and problem-tune guardrails. The
built-in `balanced` profile is also a tuned approximation; it is not a measured
SID chip profile.

### Stream Samples

Use `NewStream` for players, UIs, servers, or any integration that should pull
small chunks without rendering minutes of audio up front. This is the API a
desktop app like `rasterklang-player` should build on.

```go
stream, err := sid.NewStream(tune, sid.StreamOptions{
	Subtune:    1,
	SampleRate: 44100,
})
if err != nil {
	return err
}

samples := make([]int16, 1024)
deadline := time.Now().Add(10 * time.Second)
for time.Now().Before(deadline) {
	n, err := stream.ReadSamples(samples)
	if err != nil {
		return err
	}

	// Send samples[:n] to your audio backend, encoder, websocket, etc.
	pcmBytes := sid.SamplesToPCM16LE(samples[:n])
	_ = pcmBytes
}
```

SID files do not carry reliable song lengths, so streaming is intentionally
open-ended. The caller decides duration, seeking policy, looping, fade-out,
buffer size, and audio-device integration.

### Estimate Duration

Use `EstimateDuration` when no HVSC songlength database entry is available and
you still want a better default than a fixed play span:

```go
estimate, err := sid.EstimateDuration(tune, sid.DurationEstimateOptions{
	Subtune:         1,
	MaxDuration:     8 * time.Minute,
	WallClockBudget: 3 * time.Second,
	SampleRate:      8000,
})
if err != nil {
	return err
}

if estimate.Source != sid.DurationUnknown {
	fmt.Println(estimate.Duration, estimate.Source, estimate.Confidence)
}
```

The estimate is intentionally conservative. It can report loop detection,
trailing-silence detection, or `unknown` when the scan budget ends without a
confident signal.

For HVSC databases, load `Songlengths.md5` and look up entries by the modern
full-content fingerprint:

```go
db, err := sid.LoadSonglengthDatabase("/path/to/Songlengths.md5")
if err != nil {
	return err
}

entry, ok := db.LookupTune(tune)
if ok && len(entry.Lengths) >= int(tune.StartSong) {
	fmt.Println(entry.Lengths[tune.StartSong-1])
}
```

### Debug and Trace Streams

Use `NewDebugStream` for learning tools, inspectors, and WASM frontends that
need bounded trace events or snapshots. The normal `NewStream` path remains the
fast playback API.

```go
debug, err := sid.NewDebugStream(tune, sid.DebugOptions{
	Subtune:        1,
	SampleRate:     44100,
	TraceMask:      sid.TraceFrames | sid.TraceCPUSteps | sid.TraceSIDWrites,
	MaxTraceEvents: 4096,
})
if err != nil {
	return err
}

framePCM, err := debug.StepFrame()
if err != nil {
	return err
}
_ = framePCM

events, info := debug.ReadTrace(256, 0)
snapshot := debug.Snapshot()
_ = events
_ = info
_ = snapshot
```

Trace buffers are fixed-size and report dropped events. `DebugStream` also
supports `ReadSamples` for normal chunked rendering, `StepFrame` for frame-level
inspection, and `StepInstruction` for direct `play` address and IRQ-driven
tunes.

### Choosing an API

- Use `LoadFile` or `Parse` to read SID data.
- Use `Render` for bounded, offline work.
- Use `NewStream` and `ReadSamples` for real-time playback and app integration.
- Use `EstimateDuration` as a fallback when no external songlength metadata is
  available.
- Use `LoadSonglengthDatabase` when an HVSC `Songlengths.md5` file is available.
- Use `NewDebugStream` for bounded trace events, snapshots, and educational
  stepping tools.
- Use `SamplesToPCM16LE` when an audio backend expects little-endian PCM bytes.
- Use `WriteWAV`, `ReadWAV`, and `AnalyzePCM16` for tooling and tests.

## Current Limits

The renderer is still intentionally approximate. It can run direct `play`
address tunes and many interrupt-driven tunes, including simple RSID cases and
many BASIC RSID launchers, but it does not yet emulate a full C64 main loop,
complete BASIC/KERNAL ROM behavior, cycle-exact SID behavior, true
transistor-level combined waveforms, or a reSID-grade analog filter model.

The SID audio path does include separate 6581/8580 cutoff curves, non-linear
6581-style filter drive, model-specific mixer/output shaping, D418 sample
support, and correct voice-3-off behavior when voice 3 is routed through the
filter. It is meant to sound musical and SID-like while staying small and pure
Go; it is not yet calibrated against measured chip profiles.

For a more detailed map of which audio-engine choices are well-established SID
behavior and which are still tuned approximations, see
[docs/sid-engine-notes.md](docs/sid-engine-notes.md).

Many common undocumented 6510 opcodes are implemented, but not every unstable
silicon edge case is modeled.

The code is structured so the parser, CPU, C64 bus, SID model, streaming
renderer, and CLI can be improved independently.

## Support Matrix

| Area | Current support | Release note |
| --- | --- | --- |
| Platforms | Linux, macOS, and Windows archives for `amd64` and `arm64` | Built by the tag release workflow as `.tar.gz` archives with `.sha256` files. |
| CLI playback | macOS native playback, Linux audio backends through common player commands, Windows render/analyze/info binaries | Windows direct audio playback needs manual validation before it is documented as first-class. |
| SID formats | PSID and many RSID tunes | Unsupported RSID/BASIC/ROM edge cases report errors, unknown labels, or approximate playback instead of guaranteed fidelity. |
| C64 system behavior | Direct `play` address tunes, many IRQ-driven tunes, many BASIC RSID launchers | No full C64 main loop and no complete BASIC/KERNAL ROM behavior. |
| SID audio | 6581/8580-aware synthesis, ADSR, D418 samples, OSC3/ENV3, multimode filtering | Tuned approximation, not a measured chip model. |
| Song lengths | HVSC `Songlengths.md5` lookup plus heuristic fallback | Heuristic estimates are conservative and may return `unknown`. |
| Bundled media | None | Rasterklang does not bundle HVSC, C64 ROM images, or third-party SID files. |
| Package managers | Draft Homebrew formula and Scoop manifest generated from release checksums | No package manager formula is published yet; Scoop is the first Windows package path, while winget is deferred until a suitable Windows `.exe`/`.zip` or installer artifact exists. Use release archives or `go install` until a tap/bucket/package path exists. |

## Unsupported Tune Behavior

Rasterklang should fail plainly or label support uncertainty instead of claiming
full C64 compatibility. Unsupported RSID/BASIC/ROM edge cases can show up as:

- `info` labels such as `BASIC`, `Magic Voice`, `SAM/Reciter`, `Electronic Speech Systems`, or `Sound Master`.
- parser or loader errors for invalid or unsupported files.
- duration estimates with low confidence or `unknown`.
- approximate playback when a tune depends on unimplemented C64 system behavior,
  unstable CPU edge cases, or exact analog SID behavior.

Use HVSC type filters such as `-exclude-type "Electronic Speech Systems"` when
running compatibility scans for release metrics. Use `duration-validate` with an
HVSC `Songlengths.md5` file when you need evidence for song-length behavior.

## Troubleshooting

### Linux audio backends

Default Linux builds avoid a hard ALSA development dependency. The CLI writes
PCM through the first available command in this order: `aplay`, `ffplay`,
`paplay`, then `pw-play`. Install one of those tools if `rasterklang play`
cannot open audio.

For direct ALSA output through the Go audio backend, build locally with:

```sh
go build -tags rasterklang_alsa ./cmd/rasterklang
```

### Unsupported RSID/BASIC/ROM edge cases

Rasterklang does not ship or emulate complete C64 ROM images. Tunes that depend
on exact BASIC/KERNAL ROM calls, unusual loader behavior, or a full C64 main
loop may not start or may sound wrong. Use `rasterklang info` first to inspect
labels and support verdicts, then try `render` with a fixed `-duration` to
produce a reproducible artifact for debugging.

### C64 ROM images and HVSC

Rasterklang does not bundle C64 ROM images, HVSC, or third-party SID files. Use
your own legally obtained `.sid` collection. If a command needs song-length
metadata, pass the path to your local HVSC `Songlengths.md5`.

### Duration looks wrong

SID files do not carry authoritative song lengths. Prefer an HVSC
`Songlengths.md5` entry when available. The built-in duration heuristic is a
fallback and can return `unknown`; pass `-duration` for playback or rendering
when you need a fixed span.

### Output clips or sounds too quiet

Run `rasterklang analyze` on a rendered WAV or SID to inspect peak, RMS, DC
offset, and clipping. For playback, adjust `-volume`; for experiments, compare
profiles with `render -profile`.
