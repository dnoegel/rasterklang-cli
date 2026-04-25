# zmk-sid

Pure-Go SID engine and CLI for PSID/RSID tunes.

This is a from-scratch implementation, not a libsidplayfp wrapper. The project
currently provides:

- PSID/RSID metadata parsing
- a MOS 6502/6510 CPU core with many common undocumented opcodes
- a 64K C64 memory bus with SID register mapping
- direct `play` address and minimal IRQ-driven tune playback
- a model-aware three-voice SID path with oversampling
- ADSR, D418 sample, OSC3, ENV3, basic multimode filter, and output filtering
- offline WAV rendering and pull-based sample streaming for apps

## CLI

Download the latest build artifact from the GitHub Actions run for your
platform:

- `zmk-sid-linux-amd64`
- `zmk-sid-linux-arm64`
- `zmk-sid-macos-amd64`
- `zmk-sid-macos-arm64`

Each artifact contains a `.tar.gz` archive and a `.sha256` checksum. Unpack it
and put the binary somewhere on your `PATH`:

```sh
tar -xzf zmk-sid-linux-amd64.tar.gz
./zmk-sid-linux-amd64 play path/to/tune.sid
```

If you have Go installed, you can also install the CLI from the module:

```sh
go install github.com/dnoegel/zmk-sid/cmd/zmk-sid@latest
```

Tagged releases attach the same archives to GitHub Releases.

For development from a checkout:

```sh
go run ./cmd/zmk-sid play path/to/tune.sid
go run ./cmd/zmk-sid info path/to/tune.sid
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

Prints SID metadata and the engine's current support verdict:

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
-duration 3m    # selected playback span
-start 30s      # skip into the tune before playback
-rate 48000     # output sample rate
-volume 0.8     # playback gain multiplier
-loop           # repeat the selected span until Ctrl-C
-quiet          # suppress status output
```

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
```

### `analyze`

Reports peak, RMS, DC offset, clipped samples, crest factor, and zero crossings.
It accepts SID files or mono 16-bit WAV files:

```sh
zmk-sid analyze -duration 30s Commando.sid
zmk-sid analyze tune.wav
```

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

### Choosing an API

- Use `LoadFile` or `Parse` to read SID data.
- Use `Render` for bounded, offline work.
- Use `NewStream` and `ReadSamples` for real-time playback and app integration.
- Use `SamplesToPCM16LE` when an audio backend expects little-endian PCM bytes.
- Use `WriteWAV`, `ReadWAV`, and `AnalyzePCM16` for tooling and tests.

## Current Limits

The renderer is still intentionally approximate. It can run direct `play`
address tunes and many interrupt-driven tunes, including simple RSID cases, but
it does not yet emulate a full C64 main loop, BASIC RSID startup, ROM behavior,
cycle-exact SID behavior, true transistor-level combined waveforms, or a
reSID-grade analog filter model. Many common undocumented 6510 opcodes are
implemented, but not every unstable silicon edge case is modeled.

The code is structured so the parser, CPU, C64 bus, SID model, streaming
renderer, and CLI can be improved independently.
