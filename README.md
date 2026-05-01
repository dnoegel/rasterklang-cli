# zmk-sid

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

## CLI

Download the latest release for your platform from
<https://github.com/dnoegel/zmk-sid/releases/latest>:

- `zmk-sid-linux-amd64`
- `zmk-sid-linux-arm64`
- `zmk-sid-macos-amd64`
- `zmk-sid-macos-arm64`

Each release asset includes a `.tar.gz` archive and a `.sha256` checksum. Unpack
the archive and put the binary somewhere on your `PATH`:

```sh
tar -xzf zmk-sid-linux-amd64.tar.gz
./zmk-sid-linux-amd64 play path/to/tune.sid
```

If you have Go installed, you can also install the CLI from the module:

```sh
go install github.com/dnoegel/zmk-sid/cmd/zmk-sid@latest
```

For development from a checkout:

```sh
go run ./cmd/zmk-sid play path/to/tune.sid
go run ./cmd/zmk-sid info path/to/tune.sid
go run ./cmd/zmk-sid duration path/to/tune.sid
go run ./cmd/zmk-sid duration-validate -songlengths ~/C64Music/DOCUMENTS/Songlengths.md5 path/to/tunes
go run ./cmd/zmk-sid render -duration 30s -o tune.wav path/to/tune.sid
go run ./cmd/zmk-sid analyze -duration 30s path/to/tune.sid
```

## Releases

Create releases from a clean `main` checkout:

```sh
make test
make tag VERSION=v0.1.0
make push-tag VERSION=v0.1.0
```

Or create and push the tag in one step:

```sh
make release VERSION=v0.1.0
```

Pushing a `v*` tag triggers the release workflow, which builds Linux/macOS
archives and publishes them to the matching GitHub Release.

### `info`

Prints SID metadata, filterable tune type labels, and the engine's current
support verdict:

```sh
zmk-sid info Commando.sid
```

### `play`

Streams a tune to your audio device:

```sh
zmk-sid play -subtune 2 -duration 3m Commando.sid
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
go build -tags zmk_alsa ./cmd/zmk-sid
```

### `render`

Writes mono 16-bit WAV:

```sh
zmk-sid render -subtune 1 -duration 2m -rate 44100 -o tune.wav Commando.sid
zmk-sid render -profile profile-candidate.json -duration 30s -o candidate.wav Commando.sid
```

### `analyze`

Reports peak, RMS, DC offset, maximum sample delta, clipped samples, crest
factor, and zero crossings. It accepts SID files or mono 16-bit WAV files:

```sh
zmk-sid analyze -duration 30s Commando.sid
zmk-sid analyze tune.wav
```

### `duration`

Estimates SID playback length without a songlength database:

```sh
zmk-sid duration Commando.sid
zmk-sid duration -all Commando.sid
zmk-sid duration -budget 3s -max 8m Commando.sid
```

The heuristic renders quickly at a low sample rate, watches SID register/audio
fingerprints for repeated windows, and also detects trailing silence. Results
include the source, confidence, simulated span, and reason. This is a fallback,
not a replacement for HVSC `Songlengths` data.

To compare the heuristic against an HVSC songlength database:

```sh
zmk-sid duration-validate \
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
import sid "github.com/dnoegel/zmk-sid"
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

The default sound is the built-in `balanced` profile. For 6581 playback this is
currently the promoted `zmk-optimize` filter-focused profile from the no-prune
global optimizer run. Tools such as `zmk-optimize` can still emit JSON profile
candidates, and callers can pass those profiles into the renderer without
changing Go constants:

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
desktop app like `zmk-player` should build on.

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
