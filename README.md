# sidplayer

Pure-Go SID player experiment.

This is a from-scratch proof of concept, not a libsidplayfp wrapper. It currently
parses PSID/RSID metadata and can render many PSID/RSID tunes to mono 16-bit WAV
using:

- a small legal-opcode MOS 6502/6510 CPU core
- a 64K C64 memory bus with SID register mapping
- a minimal frame IRQ path for interrupt-driven tunes
- cycle-timed audio generation while the player routine executes
- a model-aware three-voice SID oscillator/envelope path with 4x internal
  oversampling
- basic multimode filter, D418 sample, OSC3, and ENV3 support

## Try It

```sh
go run ./cmd/sidplayer info path/to/tune.sid
go run ./cmd/sidplayer render -duration 30s -o tune.wav path/to/tune.sid
go run ./cmd/sidplayer analyze -duration 30s path/to/tune.sid
go run ./cmd/sidplayer analyze tune.wav
```

Useful render flags:

```sh
-subtune 2
-duration 2m
-rate 48000
```

`analyze` reports peak, RMS, DC offset, clipped samples, crest factor, and zero
crossings. It works on rendered SID input and on mono 16-bit WAV files, which
makes it useful for playback regressions while the engine evolves.

## Current Limits

The renderer is still intentionally approximate. It can run direct `play`
address tunes and many interrupt-driven tunes, including simple RSID cases, but
it does not yet emulate a full C64 main loop, BASIC RSID startup, ROM behavior,
cycle-exact SID behavior, true transistor-level combined waveforms, or a
reSID-grade analog filter model. Many common undocumented 6510 opcodes are
implemented, but not every unstable silicon edge case is modeled.

The code is structured so the parser, CPU, C64 bus, SID model, and CLI can be
improved independently.
