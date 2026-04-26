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
| Debug instrumentation | High | Trace hooks are opt-in and nil-checked. The normal `Stream` path does not allocate trace objects, while `DebugStream` owns bounded buffers and snapshots for tooling. |
| Duration estimation | Low-medium | SID files do not encode song lengths. HVSC `Songlengths.md5` is supported for validation/database lookup, while the fallback estimator uses bounded emulation, repeated SID/audio fingerprints, and trailing silence. |

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
- `Airwolf_Title.sid` exposed the opposite failure mode: voice 3 is routed
  through a 6581 low-pass filter with resonance 15/15 and a sweeping cutoff.
  The previous explicit-Euler state-variable filter became numerically too
  ringy in that case. The first 8 seconds peaked around 0.999 with a max sample
  delta around 0.758, while the dry audition path stayed much calmer.

Earlier state:

- The first useful filter path was a pragmatic state-variable filter fed by the
  engine's normalized voice samples.
- It used model-aware cutoff shaping and resonance damping constants, but those
  constants were musical tuning values, not values derived from SID die
  photographs, measured op-amp curves, or DAC tables.
- This worked for many tunes because it gave the output a darker 6581-like
  character, but it made a dangerous assumption: high resonance and fast cutoff
  movement would remain numerically well behaved at the audio oversample rate.
- `Airwolf_Title.sid` proved that assumption wrong. Its max-resonance 6581
  low-pass sweep drove the filter into audible ringing/crackle artifacts.

Airwolf investigation:

- Filter bypass in Insight was the first useful diagnostic. With routed voices
  presented dry, the same passage stayed much calmer, so the problem was not the
  player code, voice routing, or voice 3 off behavior.
- The first attempted mitigation reduced the audible artifacts by damping the
  6581 filter profile more aggressively. That was deliberately not kept as the
  real fix: it treated Airwolf as a loud symptom rather than explaining why the
  filter became unstable.
- The useful fix was to replace the explicit-Euler state update with a
  topology-preserving state-variable filter form. This keeps state continuous
  under fast cutoff and resonance changes and avoids the previous runaway sample
  deltas.

Changes:

- Added separate 6581 and 8580 cutoff curves.
- Added model-specific resonance, damping, input drive, output drive, filter
  leakage, and external output filtering.
- Added 6581 asymmetry and stronger non-linearity.
- Replaced the filter integrator with a topology-preserving state-variable
  filter form. This keeps filter state continuous under fast cutoff changes and
  high resonance instead of relying on explicit Euler integration.
- Retuned the 6581 profile around the new integrator, keeping resonant character
  without the previous uncontrolled ringing.
- Added an `Airwolf_Title.sid` regression check that fails if the 8 second
  render returns to uncontrolled peak or max-delta behavior.

Current magic values:

- The cutoff curves in `internal/sid/chip.go` are still fitted by hand:
  8580 uses a near-linear DAC curve and 0-12.5 kHz range, while 6581 uses a
  darker non-linear curve with a small ripple term.
- `filterProfileFor` contains tuned values for input gain, input drive,
  feedback drive, resonance curve, damping base/depth/minimum, per-mode output
  gains, and final output gain.
- `mixerProfile` contains tuned voice gain, filter leakage, mixer drive, and
  asymmetry values.
- These constants are acceptable for the current `balanced` sound, but they must
  be treated as approximations. They should not be presented as measured SID
  behavior.

Reference-render comparison:

- A 3 second batch comparison was run over `test_tunes` using zmk-sid versus
  Debian `sidplayfp` 2.4.0 / `libsidplayfp` 2.4.2 with reSIDfp. `sidplayfp -nf`
  renders were also generated to weight tunes whose reference output is actually
  filter-sensitive.
- 201 of 202 tunes completed. `Great_Giana_Sisters.sid` was excluded because
  `sidplayfp` did not terminate its 3 second WAV render in this environment.
- Raw RMS is not a good filter-tuning signal by itself: zmk-sid renders were
  roughly twice as loud as the sidplayfp CLI output in the median case. After
  normalizing band energy by RMS, filter-sensitive tunes showed a clearer color
  bias: too much low/low-mid energy, only slightly too much mid energy, and too
  little high/air energy.
- The 6581 profile was retuned conservatively from those normalized medians:
  low-pass output gain moved from 0.94 to 0.74, band-pass from 0.78 to 0.68,
  and high-pass from 0.68 to 0.82. This is a fitted approximation, not a
  measured chip profile.
- The extra-dark 6581 output stage was also relaxed from a two-pole 12.5 kHz
  low-pass to a one-pole 13.5 kHz low-pass. This keeps more high-frequency
  content than the earlier Airwolf mitigation while staying below the Airwolf
  regression's transient limit.

Expected effect:

- 6581 output should be darker, rougher, and less linear.
- 8580 output should be cleaner, with different DC and filter behavior.
- Filter-heavy tunes should retain more character.
- Airwolf Title should keep its resonant low-pass sweep without the previous
  near-clipping crackle spikes. The first 8 seconds now measure around peak
  0.714 and max delta 0.285.

Remaining uncertainty:

- These are currently tuned profiles, not measured chip profiles.
- A future API should expose named profiles rather than hiding all constants in
  code.
- The TPT integrator is a better numerical model than the old explicit update,
  but the 6581 profile constants remain tuned approximations. They still need
  validation against reference renders or measured chip profiles.

Reference engines:

- reSID and reSIDfp model the SID filter as the two-integrator-loop biquadratic
  circuit used by the chip rather than as a generic audio EQ.
- reSIDfp/libsidplayfp builds lookup tables for the filter summer, mixer,
  resonance, volume amplifier, op-amp reverse transfer, and DAC behavior. Its
  6581 model uses measured op-amp voltage transfer points, 6581 DAC
  non-linearity, VCR current modeling, and explicit 6581/8580 model separation.
- The current libsidplayfp `residfp` source also exposes filter-curve controls
  for chip variation instead of baking one hidden 6581 curve into the engine.
- VICE and libsidplayfp-derived players are useful references because they carry
  reSID/reSIDfp filter work and many years of SID regression testing.

Useful source references:

- libsidplayfp release source: `src/builders/residfp-builder/residfp/Filter*.{h,cpp}`,
  `FilterModelConfig*.{h,cpp}`, `Integrator*.{h,cpp}`, `Dac.cpp`, and
  `ExternalFilter.*`
- https://github.com/libsidplayfp/libsidplayfp
- https://github.com/libsidplayfp/libsidplayfp/releases/download/v2.16.1/libsidplayfp-2.16.1.tar.gz

### Debug and learning instrumentation

Problem:

- `zmk-learn` and future WASM tools need to explain what the engine is doing
  without importing `internal/` packages or coupling the emulator to browser UI
  concepts.
- Unbounded traces would be unsafe for browser and teaching UIs.

Changes:

- Added an opt-in public `DebugStream` API with frame, CPU, bus, SID, and audio
  trace categories.
- Added fixed-size trace rings with dropped-event accounting.
- Added snapshots for CPU registers, selected bus state, SID registers, voices,
  filter state, and engine counters.
- Added low-level bus hooks, CPU `StepWithInfo`, and read-only SID snapshot
  helpers.

Expected effect:

- `zmk-learn` can drive frame-by-frame and instruction-oriented views through
  the root Go package, including from WASM.
- Normal playback stays on `Stream`; when tracing is disabled, hooks are nil and
  trace buffers are not allocated.

Remaining uncertainty:

- Instruction stepping uses the engine's minimal direct-play and IRQ call
  contexts. It is useful for teaching and inspection, but still does not model a
  full C64 main loop around the player.
- CPU mnemonics are intentionally coarse labels. Addressing-mode text can be
  expanded later without changing the event schema.

### Audio audition controls

Problem:

- `zmk-learn` Insight needs listener-facing controls so users can isolate which
  SID voices and filter routing shape the sound they are hearing.
- These controls must not rewrite the tune's SID registers or alter CPU/player
  execution, because the register heatmap and trace should continue to show the
  original program behavior.

Changes:

- Added render-time audio controls for a three-bit voice mask and filter
  bypass.
- Muted voices still advance oscillators, envelopes, OSC3/ENV3 reads, sync, and
  ring-mod state; they are only removed at the final mixer input.
- Filter bypass keeps the filter state fed but presents routed voices as dry
  audio, so switching back to the normal filter path is an audition choice
  rather than a register mutation.
- Exposed the controls through `Stream`, `DebugStream`, and the WASM API.

Expected effect:

- Insight can default to the original mix, then let the user mute/solo voices or
  compare filtered versus dry audio while all SID register views remain honest.

Remaining uncertainty:

- Filter bypass is not SID hardware behavior. It is an educational monitoring
  layer for tooling and should remain clearly separated from emulation state.

### Duration estimation

Problem:

- SID files are executable player code and usually do not contain reliable song
  length metadata.
- A fixed playback default such as 3 minutes is often visibly wrong in a music
  client.
- HVSC songlength databases are the best source for known collection files and
  are also useful for measuring how far the heuristic is off.
- Local or unknown SIDs still need a fallback.

Changes:

- Added a public `EstimateDuration` API and a `zmk-sid duration` command.
- Added modern HVSC `Songlengths.md5` parsing, full-content SID MD5 lookup, and
  a `zmk-sid duration-validate` command.
- The estimator renders at a low sample rate, groups output into time windows,
  and tracks SID register state plus coarse audio activity.
- Repeated active windows become loop candidates; sustained silence after
  previous activity becomes an end candidate.
- `play` now estimates duration by default when `-duration` was not explicitly
  provided, with a 3 second wall-clock budget and a 3 minute fallback when the
  result remains unknown.

Expected effect:

- The CLI and future UI clients get a practical default duration for many tunes
  without blocking on an external database.
- Results include source, confidence, scanned duration, and reason so callers
  can present uncertainty honestly.
- The validator can compare whole directories against `Songlengths.md5`, report
  mismatches over a threshold, and summarize mean/max absolute error.

Remaining uncertainty:

- This is a heuristic. Repeated sections can look like loops, quiet passages can
  look like endings, and complex players may hide state outside the observed SID
  register/audio fingerprint.
- The parser targets the modern `Songlengths.md5` format where MD5 is computed
  over the full SID file. The old pre-HVSC#71 special SID MD5 format is not
  implemented.
- Future player work should use HVSC songlength entries as primary duration data
  and this estimator only as fallback.

## What Is Deliberately Approximate

- Full C64 scheduling is not implemented.
- VIC timing, raster IRQ behavior, ROM startup paths, and complete RSID
  environment behavior are still limited.
- Combined waveform behavior is not table-accurate.
- Filter behavior is not transistor-level.
- The output stage is a useful audio model, not a measured analog circuit.
- De-clicking is a listener-facing safety layer, not strict emulation.
- Duration estimation is not part of SID emulation accuracy. It is a player
  convenience layer.

## Validation So Far

Current checks include:

```sh
go test ./...
go run ./cmd/zmk-sid analyze -duration 5s ../test_tunes/Cordis_Monophonous.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/8_Bit-Maerchenland_V2.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/Alles_Luege.sid
go run ./cmd/zmk-sid analyze -duration 8s ../test_tunes/Airwolf_Title.sid
go run ./cmd/zmk-sid duration -budget 3s ../test_tunes/Alles_Luege.sid
go run ./cmd/zmk-sid duration-validate -songlengths ~/C64Music/DOCUMENTS/Songlengths.md5 ~/C64Music/MUSICIANS/H/Hubbard_Rob
```

Useful current problem tunes:

- `Cordis_Monophonous.sid`: click and note-edge behavior.
- `8_Bit-Maerchenland_V2.sid`: subtle `pulse+saw` track around 7-8 seconds.
- `Alles_Luege.sid`: CIA speed and perceived timing.
- `Airwolf_Title.sid`: max-resonance 6581 low-pass sweep on voice 3; catches
  over-aggressive filter ringing and crackle-like transients.

Metrics are not enough on their own. Peak, RMS, max delta, and zero crossings can
catch regressions, but listening tests and reference renders are still required.

## Recommended Next Steps

1. Add explicit sound profiles.

   Suggested profiles:

   - `accurate`: fewer playback-polish ramps, stricter SID semantics, and either
     reference-engine comparison or a future reSID-style filter model.
   - `balanced`: current default direction.
   - `soft`: stronger de-clicking for casual playback.

2. Add a reference comparison harness.

   Render known time windows with zmk-sid and a reference engine such as
   libsidplayfp/reSIDfp, then compare:

   - RMS and peak
   - zero crossings
   - spectral centroid or simple band energies
   - onset deltas
   - optionally reference WAV snippets checked into a separate test-data repo

   This should be comparison-only unless the project explicitly accepts the
   licensing and maintenance implications of embedding or porting GPL reSIDfp
   code.

3. Replace more heuristics with documented tables or measured curves.

   Priority order:

   - filter cutoff/resonance profiles
   - filter mixer, volume, and output-stage constants
   - `pulse+saw`
   - `saw+triangle`
   - `pulse+triangle`
   - `pulse+saw+triangle`

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
