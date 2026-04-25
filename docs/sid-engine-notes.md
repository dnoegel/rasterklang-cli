# SID Engine Notes

This document tracks why the current SID audio path behaves the way it does,
which parts are based on well-known SID behavior, and which parts are still
heuristic tuning.

The goal is not to claim reSID-level accuracy. The goal is to keep zmk-sid
honest: changes should explain the audible problem they address, the confidence
level behind them, and the validation still needed.

## Confidence Levels

| Area | Confidence | Why |
| --- | --- | --- |
| Pulse comparator polarity | High | SID pulse output is high when the oscillator accumulator is greater than or equal to the pulse-width register. This fixed missing or weak `pulse+saw` material. |
| CIA-speed tunes | High | PSID speed bit 1 means the player can set CIA Timer A. We seed from the default/init timer and refresh `$DC04/$DC05` after playback calls so player-programmed tempos are picked up. |
| PAL/NTSC clock setup | High | Tune headers define the preferred clock domain. The engine sets the CPU clock and C64 environment flags accordingly. |
| Bank register before init/play | High | Many tunes depend on `$0001` exposing the expected RAM/I/O/ROM layout for the address being called. |
| Delayed SID writes with sub-sample audio clocking | Medium-high | SID register writes should affect audio after the CPU cycles that performed them, not all at frame boundaries. This improves timing and reduces note-edge clicks. The model is still not cycle-exact. |
| Voice 3 off behavior | High | Voice 3 is muted only from the direct mixer path. It remains audible when routed through the filter. |
| OSC3 and ENV3 reads | Medium-high | Tunes can read these registers for modulation. The implementation exposes useful values, but not every pipeline delay is modeled. |
| Wave DAC zero levels | Medium | The 6581 and 8580 zero offsets follow reSID-style values (`0x380` and `0x9e0`). This is better than a symmetric DAC, but not calibrated per chip. |
| Floating waveform DAC | Medium | The hold times are based on reSID/VICE behavior: short on 6581, long on 8580. Decay shape is approximate. |
| D418 volume DAC | Medium | The volume register can carry sample playback. The current model preserves moves and ramps startup changes, but does not emulate every analog output detail. |
| ADSR envelope | Medium | The rate periods and exponential counter behavior are SID-inspired and musically useful, but edge cases such as delay bugs and all write-order quirks are incomplete. |
| Pulse+saw combined waveform | Medium | The behavior is now modeled as a bitwise pull-down shape rather than a simple product. The concept matches SID/reSID descriptions, but the `pull`, `spread`, and `threshold` constants are still tuned approximations. |
| Other combined waveforms | Low-medium | Non-`pulse+saw` combinations still use a simpler non-linear product approximation. These need more work. |
| Filter model | Low-medium | The filter is model-aware and musical, but parameters are tuned by ear and by problem tunes, not derived from measured chip profiles. |
| Mixer/output profile | Low-medium | Voice gain, filter leakage, drive, low-pass, and high-pass values are pragmatic shaping constants. They help avoid sterile output but are not measured. |
| De-click ramps | Low-medium | These are playback polish, not hardware-accurate SID behavior. They reduce obvious clicks but can soften attacks if overused. |

## Changes And Intent

### Timing and note-edge clicks

Problem:

- `Cordis_Monophonous.sid` exposed short clicks on note starts.
- Some notes clicked more when all register changes landed at the same rendered sample.

Changes:

- SID writes can be delayed until the CPU cycles that caused them have been
  rendered.
- The engine renders at the SID oversample step instead of only once per output
  sample.
- Frequency smoothing was removed. SID frequency register changes are digital
  and immediate; smoothing made some tunes feel sluggish.

Expected effect:

- Register changes land closer to their real timing.
- Note starts should click less because audio no longer sees whole-frame jumps.
- Timing-sensitive tunes should feel less lazy.

Remaining uncertainty:

- This is still not a full cycle-exact C64/SID scheduler.
- The de-click ramps can hide emulator roughness but may also reduce attack
  sharpness. They should become configurable profiles.

### Missing 8 Bit Maerchenland voice

Problem:

- In `8_Bit-Maerchenland_V2.sid`, a subtle track should appear around 7-8
  seconds.
- Register tracing showed voice 2 using `ctrl=$61` (`pulse+saw+gate`) with a
  very narrow pulse width, routed through a high-resonance band/high-pass filter
  (`res/filt=$f2`, `mode/vol=$6f`).

Changes:

- Fixed pulse comparator polarity.
- Added a dedicated `pulse+saw` combined waveform path.
- Replaced the old smooth product approximation for this waveform with a
  bitwise pull-down model.

Expected effect:

- Narrow pulse widths no longer suppress nearly the whole `pulse+saw` voice.
- The 7-8s section gains high-frequency combined-wave structure without simply
  becoming louder.

Remaining uncertainty:

- The pull-down constants are approximations. They should eventually be replaced
  by measured tables, generated tables, or a better calibrated model.
- Other combined waveforms still need their own treatment.

### Alles Luege timing

Problem:

- `Alles_Luege.sid` felt slower or more sluggish than reSID.
- The Welle player used by the tune writes CIA Timer A during the first `play`
  call, not during `init`.
- The old engine only read `$DC04/$DC05` once after `init`, so it kept the PAL
  default `$4025` period instead of the tune-programmed `$3200` period.

Changes:

- Removed oscillator frequency smoothing.
- CIA Timer A playback speed is seeded after init for CIA-speed tunes, then
  refreshed after playback calls.
- Bank register setup and PAL environment setup were tightened.

Expected effect:

- Digital note changes happen immediately.
- CIA-speed tunes use the tune-programmed frame period instead of staying on a
  fixed video-frame fallback.
- `Alles_Luege.sid` advances playback at roughly the intended 77 Hz after its
  first `play` call instead of dragging at roughly 60 Hz.

Remaining uncertainty:

- Timer changes are applied to the next rendered frame. The current engine still
  does not emulate the full CIA underflow, latch, and force-load state machine.
- If a tune relies on more complete CIA/VIC/IRQ behavior, the current minimal
  environment can still drift from a full emulator.

### Filter and output character

Problem:

- A purely clean digital oscillator path sounded too sterile and could hide the
  character of 6581/8580 material.

Changes:

- Added separate 6581 and 8580 cutoff curves.
- Added model-specific resonance, damping, input drive, output drive, filter
  leakage, and external output filtering.
- Added 6581 asymmetry and stronger non-linearity.

Expected effect:

- 6581 output should be darker, rougher, and less linear.
- 8580 output should be cleaner, with different DC and filter behavior.
- Filter-heavy tunes should retain more character.

Remaining uncertainty:

- These are currently tuned profiles, not measured chip profiles.
- A future API should expose named profiles rather than hiding all constants in
  code.

## What Is Deliberately Approximate

- Full C64 scheduling is not implemented.
- VIC timing, raster IRQ behavior, ROM startup paths, and complete RSID
  environment behavior are still limited.
- Combined waveform behavior is not table-accurate.
- Filter behavior is not transistor-level.
- The output stage is a useful audio model, not a measured analog circuit.
- De-clicking is a listener-facing safety layer, not strict emulation.

## Validation So Far

Current checks include:

```sh
go test ./...
go run ./cmd/zmk-sid analyze -duration 5s ../test_tunes/Cordis_Monophonous.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/8_Bit-Maerchenland_V2.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/Alles_Luege.sid
```

Useful current problem tunes:

- `Cordis_Monophonous.sid`: click and note-edge behavior.
- `8_Bit-Maerchenland_V2.sid`: subtle `pulse+saw` track around 7-8 seconds.
- `Alles_Luege.sid`: CIA speed and perceived timing.

Metrics are not enough on their own. Peak, RMS, max delta, and zero crossings can
catch regressions, but listening tests and reference renders are still required.

## Recommended Next Steps

1. Add explicit sound profiles.

   Suggested profiles:

   - `accurate`: fewer playback-polish ramps, stricter SID semantics.
   - `balanced`: current default direction.
   - `soft`: stronger de-clicking for casual playback.

2. Add a reference comparison harness.

   Render known time windows with zmk-sid and a reference engine, then compare:

   - RMS and peak
   - zero crossings
   - spectral centroid or simple band energies
   - onset deltas
   - optionally reference WAV snippets checked into a separate test-data repo

3. Replace more heuristics with documented tables or measured curves.

   Priority order:

   - `pulse+saw`
   - `saw+triangle`
   - `pulse+triangle`
   - `pulse+saw+triangle`
   - filter cutoff/resonance profiles

4. Make de-click behavior configurable.

   De-clicking is valuable for a music player, but engine users should be able
   to choose less smoothing when comparing against hardware or reSID.

5. Keep tune-specific notes.

   When a tune exposes a bug, document:

   - the audible symptom
   - relevant SID register state
   - the code change
   - expected improvement
   - remaining uncertainty
