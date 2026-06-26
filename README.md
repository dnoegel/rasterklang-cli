# rasterklang

[![Build binaries](https://github.com/dnoegel/rasterklang-cli/actions/workflows/binaries.yml/badge.svg)](https://github.com/dnoegel/rasterklang-cli/actions/workflows/binaries.yml)
[![Release](https://github.com/dnoegel/rasterklang-cli/actions/workflows/release.yml/badge.svg)](https://github.com/dnoegel/rasterklang-cli/actions/workflows/release.yml)

Rasterklang is a pure-Go SID engine, Go library, and command-line player for
PSID/RSID tunes. It is a from-scratch implementation, not a libsidplayfp
wrapper.

It focuses on a small, hackable engine that can parse SID metadata, render audio
offline, stream samples to applications, and power the Rasterklang WASM and
desktop players.

## What It Does

- Plays and renders many PSID and RSID tunes.
- Prints SID metadata and support labels such as `RSID`, `BASIC`, `Magic Voice`,
  `SAM/Reciter`, `Electronic Speech Systems`, and `Sound Master`.
- Provides a MOS 6502/6510 CPU core, C64 memory bus, SID register mapping, and
  model-aware three-voice SID audio path.
- Supports ADSR, D418 samples, OSC3, ENV3, multimode filtering, output filtering,
  WAV rendering, duration estimation, and HVSC `Songlengths.md5` lookup.
- Exposes a Go API for apps that need parsing, rendering, streaming, analysis,
  or debug/trace tools.

Rasterklang does not bundle HVSC, C64 ROM images, or third-party SID files. Use
your own legally obtained `.sid` collection.

## Install

Download a release archive from:

<https://github.com/dnoegel/rasterklang-cli/releases/latest>

Unpack it and run the binary:

```sh
tar -xzf rasterklang-linux-amd64.tar.gz
./rasterklang-linux-amd64/rasterklang --version
./rasterklang-linux-amd64/rasterklang play path/to/tune.sid
```

If you have Go installed, you can also install from the module:

```sh
go install github.com/dnoegel/rasterklang-cli/cmd/rasterklang@latest
```

Package-manager channels are planned, but the release archive and `go install`
paths are the canonical install options until those channels are live.

## CLI Quickstart

```sh
rasterklang info Commando.sid
rasterklang play -subtune 2 -duration 3m Commando.sid
rasterklang render -duration 30s -o preview.wav Commando.sid
rasterklang analyze preview.wav
rasterklang duration Commando.sid
```

Useful playback flags:

```text
-subtune 2      1-based subtune number
-duration 3m    selected playback span; use 0 to play until interrupted
-start 30s      skip into the tune before playback
-rate 48000     output sample rate
-profile path   sound profile name or JSON path; defaults to balanced
-volume 0.8     playback gain multiplier
-loop           repeat the selected span until Ctrl-C
```

On macOS, playback uses the Go audio backend directly. On Linux, default builds
stream raw PCM to the first available command among `aplay`, `ffplay`, `paplay`,
and `pw-play`. For direct ALSA output, build locally with:

```sh
go build -tags rasterklang_alsa ./cmd/rasterklang
```

## Go Library

Import the root package:

```go
import sid "github.com/dnoegel/rasterklang-cli"
```

Load metadata:

```go
tune, err := sid.LoadFile("Commando.sid")
if err != nil {
	return err
}

fmt.Println(tune.Title, tune.Author, tune.Songs)
```

Render audio:

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

Stream samples for an app or player:

```go
stream, err := sid.NewStream(tune, sid.StreamOptions{
	Subtune:    1,
	SampleRate: 44100,
})
if err != nil {
	return err
}

samples := make([]int16, 1024)
n, err := stream.ReadSamples(samples)
_ = samples[:n]
_ = err
```

Use `NewDebugStream` for learning tools, frame stepping, bounded trace events,
and snapshots.

## Current Limits

Rasterklang is still an approximate SID engine. It can run direct `play` address
tunes and many interrupt-driven tunes, including simple RSID cases and many
BASIC RSID launchers, but it is not a full C64 emulator.

Known limits:

- no complete C64 main loop
- no bundled BASIC/KERNAL ROM behavior
- no cycle-exact SID model
- no transistor-level combined waveform model
- tuned filter/output profiles rather than measured chip profiles
- Windows direct audio playback still needs first-class validation

For more detail on SID-engine choices and approximations, see
[docs/sid-engine-notes.md](docs/sid-engine-notes.md).

## Development

```sh
go test ./...
make check
go run ./cmd/rasterklang info path/to/tune.sid
go run ./cmd/rasterklang render -duration 30s -o tune.wav path/to/tune.sid
```

The local `make check` target runs formatting checks, static analysis when
available, release artifact helpers, `go vet`, and tests.

Maintainer release notes live in [docs/release.md](docs/release.md).
