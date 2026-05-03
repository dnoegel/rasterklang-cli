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
| Triangle waveform color | Medium | 6581 triangle uses 12-bit accumulator-derived stair steps. The earlier default saw bleed was removed after harmonic analysis showed it over-emphasized even harmonics versus sidplayfp; bleed remains profile-controlled. |
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
  gains, BP/HP cutoff-dependent response tilt, and final output gain.
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
- Sound profiles now support an optional `filter.modeResponseDB.lowPass`
  correction, matching the existing BP/HP response-profile mechanism. The
  built-in `balanced` profile still has no LP response correction; this field is
  a profile/optimizer surface for fitted candidates. It is an approximation,
  not established SID behavior, and should be accepted only after sweep and tune
  guardrails show improvement.

Synthetic filter sweep harness:

- Added `cmd/filter-sweep` as an offline comparison tool for controlled filter
  stimuli. It generates small PSID v2 files rather than importing any
  sidplayfp/reSIDfp code, renders the same files through zmk-sid and an
  installed `sidplayfp` binary, then writes CSV/JSON metrics.
- The generated stimuli use one routed voice, configurable waveform/frequency,
  fixed model, fixed filter mode, one resonance value, and one cutoff value.
  The default sweep is a 6581 saw at 220 Hz over low-pass, band-pass, and
  high-pass modes, resonance values 0, 4, 8, 12, and 15, and cutoff values
  0..2047 in steps of 128.
- The analysis skips the initial attack/filter settling window, then compares
  RMS, peak, spectral centroid, normalized band shares, log spectral distance,
  and normalized band-share distance. This is intended to show filter curve
  shape; absolute RMS is still tracked but should not drive filter tuning alone.
- Added `cmd/filter-sweep-report` to turn a sweep JSON or `latest-summary.json`
  into a self-contained HTML report. The report shows normalized RMS response
  curves for zmk-sid versus sidplayfp, centroid/spectral/band-distance curves,
  and per-mode median band-ratio heatmaps.
- Added `cmd/filter-sweep -dry-normalize`. This renders a matching unrouted
  dry stimulus and reports filter ratios after removing the dry oscillator,
  mixer, and output-stage level difference between zmk-sid and sidplayfp.
  Without this correction the filter sweep mostly says that zmk-sid's direct
  oscillator path is louder than sidplayfp's CLI output.
- The first default run completed 255/255 stimuli:

  ```
  go run ./cmd/filter-sweep -out ../test-results/filter-sweeps/default
  ```

  It produced a median RMS ratio of about 2.34, median spectral distance about
  2.46, and median band distance about 1.17 versus `sidplayfp`/reSIDfp. Median
  normalized band ratios were approximately sub 0.36, low 0.78, mid 1.37, high
  0.75, and air 0.16. These aggregate numbers are a smoke signal only; tuning
  should inspect per-mode/per-resonance cutoff curves instead of collapsing all
  filter cases into one median.
- The first generated report is:

  ```
  go run ./cmd/filter-sweep-report -in ../test-results/filter-sweeps/default/latest-summary.json -o ../test-results/filter-sweeps/default/report.html
  ```
- The dry-normalized sweep showed that the existing 6581 cutoff curve was a
  better fit than a more aggressive right-shifted candidate, but the per-mode
  filter gains were off relative to the dry voice path. The 6581 mode gains
  were therefore retuned from low-pass 0.74 to 0.62, band-pass 0.68 to 1.06,
  and high-pass 0.82 to 0.46. This is still a fitted approximation, not a
  measured SID profile.
- On the default 6581 saw sweep, the dry-corrected median RMS ratio moved from
  about 1.17 to 1.04, and dry-corrected band distance from about 1.14 to 0.97.
  Per-mode dry-corrected median RMS moved from LP/BP/HP about
  1.25/0.63/1.84 to about 1.04/0.97/1.07. A sparse pulse spot-check was close
  enough for a guardrail, while triangle remained less well aligned and needs a
  dedicated waveform pass before using it as a tuning driver.
- A 3 second `test_tunes` guardrail rerendered the same 201 tunes against the
  earlier sidplayfp reference ratios. The all-tune median RMS ratio moved from
  about 1.95 to 1.88, and the filter-heavy subset moved from about 2.02 to
  1.86. This was an RMS/peak guardrail only; the synthetic dry sweep remains the
  primary filter-shape metric for this change.

Real hardware recording comparison:

- Added `cmd/recording-compare` as a validation harness for real C64 recordings.
  It renders a local SID through zmk-sid, decodes a reference recording through
  `ffmpeg` when needed, aligns both streams by a 10 ms RMS envelope
  cross-correlation, optionally writes aligned WAV snippets, and reports JSON
  metrics for RMS, peak, spectral centroid, band share, band ratios, and
  spectral distance.
- This tool is deliberately a validation harness, not a license shortcut. Real
  recordings can be downloaded and compared outside the repository, but the repo
  should not vendor copyrighted MP3/FLAC recordings. The harness only depends on
  local files and does not embed any SOASC, reSID, or libsidplayfp code.
- Useful public reference sources found so far:

  - SOASC= / Stone Oakvalley's Authentic SID Collection:
    https://www.6581-8580.com/
  - SOASC search/download interface:
    https://www.6581-8580.com/socse/
  - C64Audio SID Effects:
    https://c64audio.com/products/sid-effects-double-album-free

- SOASC is the best fit for tune-by-tune validation because the search database
  exposes SID path, title, subtune count, chip revision, PAL/NTSC, song length,
  and direct MP3/FLAC links. The site describes the archive as real unmodified
  C64 hardware recordings for MOS6581R2/R3/R4 and CSG8580R5, with PAL and NTSC
  preserved.
- First SOASC smoke comparison used the exact local problem tune:

  ```
  go run ./cmd/recording-compare \
    -sid ../test_tunes/C64Music/MUSICIANS/S/SoedeSoft/Soede_Jeroen/Airwolf_Title.sid \
    -ref ../test-results/recording-compare/soasc-airwolf-title/Airwolf_Title_T001.sid_MOS6581R3.mp3 \
    -duration 30s \
    -max-lag 8s \
    -o ../test-results/recording-compare/soasc-airwolf-title/airwolf-title-r3-30s.json \
    -aligned-dir ../test-results/recording-compare/soasc-airwolf-title/aligned
  ```

  SOASC search identified it as `Airwolf Title`, Jeroen Soede, PAL,
  `MUSICIANS/S/SoedeSoft/Soede_Jeroen`, HVSC49, song length 0:43, with MP3
  recordings for MOS6581R3, MOS6581R4, and CSG8580R5. The MOS6581R3 MP3 is
  about 1.2 MB.
- First 30 second zmk-sid versus SOASC R3 result:

  - alignment lag: -0.060 s, envelope correlation about 0.389
  - RMS ratio: about 0.828 (-1.64 dB)
  - spectral distance: about 1.045
  - centroid ratio: about 0.908
  - band-share ratios: sub 0.97, low 1.25, mid 0.95, high 0.58, air 0.16

  Shorter and later windows were similar: 10 seconds from the start gave RMS
  ratio about 0.864 and correlation about 0.400; a 20 second window starting at
  8 seconds gave RMS ratio about 0.815 and correlation about 0.461.
- Interpretation: this confirms the direction of the dry sweep and sidplayfp
  comparisons. Airwolf Title is still low/low-mid heavy and high/air light
  versus a real 6581R3 recording, even after the Airwolf artifact fix and
  dry-normalized mode-gain retune. Because this first real-reference run uses
  MP3 and only moderate envelope correlation, it should be treated as a
  validation signal, not as the sole optimizer target.
- ML is not the first tool to reach for here. The current problem is a small
  number of interpretable analog parameters: cutoff curve, resonance response,
  per-mode gain, leakage, drive, output filtering, and chip-profile variation.
  Use synthetic sweeps, sidplayfp/reSIDfp comparisons, and real recordings to
  fit those parameters with classical search or Bayesian/CMA-style optimization.
  A black-box model trained on full songs would be harder to debug, would learn
  recording-chain and MP3 artifacts, and would not explain which SID behavior was
  improved.

HVSC corpus switch:

- The local `../test_tunes` directory now contains the full HVSC-style
  `C64Music` tree. The previous 202 flat top-level SID fixtures were moved out
  of the active tune root to:

  ```
  ../test-results/legacy-test-tunes-top-level
  ```

  This avoids duplicate corpus discovery and lets validation use stable HVSC
  paths.
- Added `cmd/soasc-corpus` to collect larger real-hardware comparison corpora.
  For local HVSC paths it does not need the SOASC search page: it probes the
  public SOASC mirror directly by HVSC version, recording format, subtune, and
  chip revision, then downloads selected recordings outside the repository and
  optionally runs `cmd/recording-compare`.
- First larger SOASC runs:

  ```
  go run ./cmd/soasc-corpus \
    -tunes ../test_tunes/C64Music/MUSICIANS/H/Hubbard_Rob \
    -out ../test-results/recording-compare/soasc-hubbard-r3-mp3-20 \
    -format mp3 -chips auto -limit 20 -compare -duration 20s

  go run ./cmd/soasc-corpus \
    -tunes ../test_tunes/C64Music/MUSICIANS/G/Galway_Martin \
    -out ../test-results/recording-compare/soasc-galway-r3-mp3-20 \
    -format mp3 -chips auto -limit 20 -compare -duration 20s
  ```

  Results:

  - Hubbard: 20 tunes scanned, 16 matched/downloaded/compared. Median RMS ratio
    about 0.798 (-1.96 dB), median envelope correlation about 0.373, median
    spectral distance about 1.287. Median band-share ratios were sub 1.30,
    low 1.06, mid 0.70, high 0.26, air 0.07.
  - Galway: 20 tunes scanned, 20 matched/downloaded, 19 compared. One tune
    (`Game_Over.sid`) failed in the current RSID/minimal-environment path.
    Median RMS ratio about 0.751 (-2.49 dB), median envelope correlation about
    0.625, median spectral distance about 1.124. Median band-share ratios were
    sub 1.07, low 1.01, mid 0.83, high 0.57, air 0.11.
  - Combined: 35 comparisons, 16 with envelope correlation >= 0.5. Overall
    median RMS ratio about 0.774 (-2.22 dB), high-correlation median RMS ratio
    about 0.774 (-2.23 dB). High-correlation median band-share ratios were
    sub 1.17, low 1.05, mid 0.85, high 0.56, air 0.11.

Classical conclusion so far:

- Do not tune global output gain from SOASC MP3s alone. Their recording/mastering
  level is not the same target as `sidplayfp` CLI WAV output.
- Do not tune `air` directly yet. It is heavily affected by real recording noise
  and MP3 artifacts.
- The consistent `high` deficit in better-aligned SOASC comparisons is still
  important. It suggests the current balanced profile is too dark in full songs,
  but the fix should be fitted against dry-normalized synthetic sweeps and then
  checked against SOASC, not guessed from full-song spectra alone.
- The first classical response-shape step is now applied: BP/HP get a
  cutoff-dependent 6581 mode-response tilt derived from the dry-normalized saw
  curves. The score still combines dry-normalized sweep curves, Airwolf
  peak/max-delta guardrails, and high-correlation SOASC color guardrails. The
  broader fitting plan now lives in `../zmk-optimize/docs/ml-filter-plan.md`.

Noise and curve alignment:

- Noise can be mostly removed from the filter analysis, but the clean target is
  not a whole-song spectrum. The reliable object to align is the transfer curve:

  ```
  filtered output / matching dry output
  ```

  That is why `cmd/filter-sweep -dry-normalize` is the primary filter signal.
  For real SOASC recordings, `air` is now treated as a diagnostic/noise-heavy
  band rather than a direct tuning target.
- Added `cmd/filter-curve-align`. It reads a dry-normalized sweep JSON or
  `latest-summary.json`, builds dB transfer curves for zmk-sid and sidplayfp,
  clamps a configurable floor such as `-70 dB` so stop-band/noise tails do not
  dominate, and then fits a simple affine raw-cutoff mapping plus gain offset
  per model/wave/mode/resonance group.
- Added `cmd/filter-response-fit`. It reads the same dry-normalized sweep data
  and fits small dB correction models against the transfer error:
  constant, cutoff-linear, cutoff+resonance, and cutoff-quadratic+resonance. It
  reports train/test RMSE so a more complex correction is only accepted when it
  also improves held-out cutoff/resonance buckets.
- Current 6581 saw sweep result:

  ```
  go run ./cmd/filter-curve-align \
    -in ../test-results/filter-sweeps/retune2-dry/latest-summary.json \
    -o ../test-results/filter-sweeps/retune2-dry/curve-align.json \
    -floor-db -70
  ```

  Median RMSE moves from about 3.48 dB to 2.86 dB after simple cutoff/gain
  alignment. Low-pass is already close: roughly 0.7-1.0 dB before alignment and
  down to 0.33 dB for the max-resonance Airwolf-like low-pass group. Band-pass
  and high-pass remain around 2.8-3.7 dB even after alignment.
- Interpretation: this is no longer mainly a noise problem. Once dry level and
  stop-band floor are handled, low-pass is reasonably aligned, but BP/HP need
  real profile work: resonance/Q behavior, per-mode gain, leakage, and possibly
  cutoff mapping by mode/resonance. A single global output gain or a single
  cutoff shift will not make all curves overlap.
- Sparse pulse and triangle smoke sweeps confirm that waveform differences are
  not solved by filter alignment alone. Pulse low-pass at high resonance is
  close, but pulse BP/HP remain poor. Triangle is dominated by waveform/output
  mismatch and needs a dedicated waveform pass before it should drive filter
  fitting.

BP/HP response fit:

- Curve points showed the same BP/HP failure shape across resonance: zmk-sid was
  too loud at low cutoff and too quiet at high cutoff. On the 6581 saw sweep the
  combined BP/HP dry-transfer RMSE was about 3.82 dB.
- An offline fit over the existing sweep found that a simple linear gain
  compensation from `-6 dB` at cutoff 0 to `+6 dB` at cutoff 2047 reduced the
  combined BP/HP dry-transfer RMSE to about 0.96 dB. This is implemented as a
  6581-only BP/HP mode-response tilt. It is not claimed as measured hardware
  behavior; it is a documented approximation that compensates the current
  simplified SVF/output model.
- After applying the tilt and rerunning the full 255-point 6581 saw sweep:

  - dry-corrected median RMS ratio moved from about 1.043 to 0.988
  - curve-align median RMSE moved from about 3.48 dB to 0.82 dB
  - combined BP/HP transfer RMSE moved from about 3.82 dB to 1.04 dB
  - Airwolf Title remained stable: peak about 0.653 and max delta about 0.249
  - SOASC Hubbard/Galway full-song guardrail medians stayed effectively stable

- Pulse smoke improved materially: curve-align median RMSE moved from about
  6.90 dB to 2.72 dB. Triangle smoke improved from about 9.97 dB to 6.16 dB but
  remains waveform-dominated, so it is explicitly not used as a filter tuning
  target yet.
- A second fitting pass used `cmd/filter-response-fit` on the `tilt1-dry` sweep.
  The held-out test split showed real remaining BP/HP improvement from adding
  resonance and gentle quadratic cutoff terms:

  ```
  go run ./cmd/filter-response-fit \
    -in ../test-results/filter-sweeps/tilt1-dry/latest-summary.json \
    -o ../test-results/filter-sweeps/tilt1-dry/response-fit.json \
    -floor-db -70
  ```

  Best residual correction models from that pass:

  - BP cutoff-quadratic+resonance: all RMSE about 0.50 dB, test RMSE about
    0.52 dB.
  - HP cutoff-quadratic+resonance: all RMSE about 0.73 dB, test RMSE about
    0.75 dB.
  - LP also had a mathematical residual fit, but it was deliberately not applied
    because the low-pass path is already the Airwolf-sensitive path and should
    not be retuned from a saw-only fit without a stronger audio reason.

- The 6581 BP/HP response now uses fitted dB polynomials over normalized cutoff
  and resonance. The BP total coefficients are approximately base `-3.594`,
  cutoff `+5.613`, cutoff squared `+3.784`, resonance `-2.776`, and
  cutoff*resonance `+5.603`. The HP coefficients are approximately base
  `-6.182`, cutoff `+8.207`, cutoff squared `+4.070`, resonance `+0.269`, and
  cutoff*resonance `+2.267`. These are still tuned approximations, not measured
  chip constants.
- After applying this second pass and rerunning the full 255-point 6581 saw
  sweep:

  - dry-corrected median RMS ratio stayed essentially centered: about 1.002
  - curve-align median RMSE moved from about 0.82 dB to 0.66 dB before affine
    alignment, and from about 0.75 dB to 0.53 dB after alignment
  - BP groups are now roughly 0.31-0.63 dB before alignment and 0.27-0.53 dB
    after alignment
  - HP groups are now roughly 0.47-0.75 dB before alignment and 0.45-0.73 dB
    after alignment
  - Pulse smoke improved again: curve-align median RMSE moved from about
    2.72 dB to 2.26 dB before alignment, and from about 2.69 dB to 2.19 dB after
    alignment
  - Triangle remains waveform-dominated: the dry-corrected band distance moved
    slightly better, about 2.40 to 2.35, while curve-align stayed around
    6.3 dB. Do not use triangle sweep RMSE as a filter-fit driver yet.
  - Airwolf Title remained stable: peak about 0.653, max delta about 0.249, and
    no clipping.
  - The Hubbard/Galway SOASC MP3 guardrail stayed effectively unchanged across
    35 comparisons. The high-correlation subset still has median RMS about
    0.774 (-2.23 dB), median envelope correlation about 0.688, and median
    band-share ratios around sub 1.17, low 1.05, mid 0.85, high 0.56, air 0.11.

Expected effect:

- 6581 output should be darker, rougher, and less linear.
- 8580 output should be cleaner, with different DC and filter behavior.
- Filter-heavy tunes should retain more character.
- BP/HP-heavy material should no longer have the previous too-steep transfer
  response where low cutoff was over-present and high cutoff was under-present.
- BP/HP resonance sweeps should be closer to libsidplayfp/reSIDfp without
  shifting the already-stable low-pass/Airwolf behavior.
- Airwolf Title should keep its resonant low-pass sweep without the previous
  near-clipping crackle spikes. The first 8 seconds now measure around peak
  0.653 and max delta 0.249 after the dry-normalized mode-gain and BP/HP
  response-fit retune.

Remaining uncertainty:

- These are currently tuned profiles, not measured chip profiles.
- A future API should expose named profiles rather than hiding all constants in
  code.
- The TPT integrator is a better numerical model than the old explicit update,
  but the 6581 profile constants remain tuned approximations. They still need
  validation against reference renders or measured chip profiles.
- The current filter tuning is still strongest for saw-based filter stimuli.
  Pulse moves in the right direction. Triangle dry color is much closer after
  the waveform pass below, but filtered triangle sweeps remain waveform- and
  harmonic-distribution dominated and should not drive filter constants alone.

Waveform and output color pass:

- Added `dry` as a first-class `cmd/filter-sweep -mode` value. This writes real
  dry comparison records, rather than hiding dry renders only inside
  `-dry-normalize` filter records. It is used for waveform/output color checks:

  ```
  go run ./cmd/filter-sweep \
    -mode dry -resonance 0 -cutoff 0 \
    -wave triangle -freq 220 \
    -duration 1s -skip 250ms \
    -out ../test-results/waveform-sweeps/dry-triangle-220-wave8
  ```

- The dry waveform checks showed that the earlier output was too dark, and that
  triangle was additionally too pure/sinusoidal relative to sidplayfp. At
  220 Hz, dry triangle had band distance about 2.49, with high-band ratio about
  0.15 and air ratio about 0.006. Dry saw and pulse were also dark: high-band
  ratios about 0.54 and 0.59.
- Added `cmd/waveform-harmonics`. It renders the same synthetic SID stimuli as
  the sweep harness, but scores exact harmonics (`n * f0`) instead of broad
  frequency bands. For filtered modes it also renders the matching dry stimulus
  and reports a per-harmonic `filtered/dry` transfer error. This separates
  three questions that the old band-distance metric mixed together:

  - dry waveform harmonic shape
  - filter transfer over the waveform's actual harmonics
  - broad output color

- Triangle now uses a 12-bit accumulator-derived stair-step waveform rather than
  a smooth floating-point triangle ramp. An earlier pass mixed in a small
  6581 saw-wave bleed before the voice DAC, but this is no longer part of the
  built-in `balanced` profile. `waveform.triangleSawBleed` remains available
  for explicit profile experiments.
- The extra 6581 triangle cubic smoothing was removed. It made the triangle
  too sine-like against sidplayfp.
- The 6581 voice-DAC smoothing cutoff moved from `8200 Hz` to `11500 Hz`, and
  the external 6581 output low-pass moved from `13500 Hz` to `15500 Hz`. A more
  open `18000 Hz` output made Airwolf's max-delta guardrail too close to the
  failure threshold, so `15500 Hz` is the current compromise.
- Final dry waveform spot checks:

  - Triangle 220 Hz band distance improved from about 2.49 to about 0.73; high
    ratio moved from about 0.15 to about 0.73, and air from about 0.006 to about
    0.20.
  - Triangle 55 Hz band distance improved from about 3.56 to about 0.71.
  - Triangle 880 Hz band distance is about 0.93; the higher-frequency triangle
    remains less perfect but no longer looks like the same failure mode.
  - Saw 220 Hz band distance improved from about 0.94 to about 0.65; high ratio
    moved from about 0.54 to about 0.68.
  - Pulse 220 Hz band distance improved from about 0.86 to about 0.58; high
    ratio moved from about 0.59 to about 0.73.

- Filter guardrails after the waveform/output pass:

  - Full 255-point saw sweep dry-corrected RMS is about 1.08. Curve-align median
    RMSE is about 0.77 dB before affine alignment and about 0.40 dB after
    alignment. This is a small unaligned regression versus the previous saw-only
    filter fit, but still within the current tuned-profile band, and the aligned
    shape is better.
  - The broad Triangle smoke band-distance metric is no longer the main
    optimizer. With the harmonic-driven `5%` bleed, dry triangle is less bright
    than the earlier `6%` pass but has a better harmonic structure. The smoke
    dry-corrected band distance is about 2.36 and curve-align median RMSE is
    about 6.13 dB. This confirms that the filtered triangle path still needs a
    deeper waveform/filter interaction pass.
  - Pulse smoke curve-align median RMSE moved from about 2.26 dB to about
    2.03 dB before alignment and from about 2.19 dB to about 1.96 dB after
    alignment.
  - Airwolf Title remains below the artifact guardrail: the first 8 seconds now
    measure peak about 0.654, max delta about 0.302, and no clipping.

- Real-recording SOASC guardrail after the waveform/output pass:

  - Hubbard/Galway MP3 corpus still has 35 comparisons and 16 high-correlation
    comparisons.
  - Overall median spectral distance improved from about 1.24 to about 1.10.
  - High-correlation median spectral distance improved from about 1.15 to about
    1.08.
  - High-correlation high-band ratio improved from about 0.56 to about 0.69.
    Air moved from about 0.11 to about 0.21. These bands are still low, but the
    direction matches the dry waveform/output finding.
- Harmonic findings after adding `cmd/waveform-harmonics`:

  ```
  go run ./cmd/waveform-harmonics \
    -wave triangle \
    -freq 55,110,220,440,880 \
    -mode dry,lp,bp,hp \
    -resonance 0,8,15 \
    -cutoff 0,256,512,1024,1536,2047 \
    -harmonics 32 \
    -duration 2s -skip 750ms \
    -out ../test-results/waveform-harmonics/triangle-bleed5
  ```

  The first harmonic run showed that `6%` saw bleed made broad dry bands look
  good but over-emphasized even harmonics. Reducing the bleed to `5%` improved
  the median dry harmonic-shape RMSE from about 13.77 dB to about 12.81 dB. It
  also improved the full triangle harmonic median shape RMSE from about
  15.77 dB to about 14.68 dB and transfer RMSE from about 9.12 dB to about
  8.99 dB. BP transfer improved from about 9.13 dB to about 8.99 dB; HP
  transfer improved from about 9.80 dB to about 9.77 dB; LP transfer moved
  slightly worse, from about 5.64 dB to about 5.68 dB.
- A later dry harmonic scan over triangle at 55, 110, 220, 440, and 880 Hz
  showed the opposite conclusion for the balanced default: `5%` bleed still
  over-emphasized even harmonics. Disabling the bleed improved median dry
  triangle harmonic-shape RMSE from roughly 12.6 dB to roughly 6.45 dB. Small
  intermediate values were worse than zero in that scan: `1%` measured roughly
  7.19 dB and `2%` roughly 8.92 dB.
- Interpretation: the balanced profile should keep triangle as a pure 12-bit
  stair-step waveform for now. If a later real-chip profile needs 6581 triangle
  bleed, it should be introduced as a named profile with recording guardrails,
  not as the default approximation.

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
- Added an opt-in `TraceBASIC` category for BASIC RSID debugging. It emits
  `basic.statement` events with BASIC line, token position, decoded operation
  name/text, and the runner's current estimated statement cycles. This is
  instrumentation only; it does not alter playback timing.

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
- BASIC statement traces expose the clean-room runner's estimated timing, not
  measured C64 ROM instruction timing. They are intended for VICE comparison
  tooling and calibration, not as a cycle-exact BASIC interpreter trace.

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

### Minimal KERNAL, BRK, and ROM diagnostics

Problem:

- The HVSC compatibility scan exposed several failure buckets around RSID
  startup and IRQ handling: fatal `BRK`, unsupported opcodes in `$EAxx`, and
  raw RAM under `$FFFE/$FFFF` being mistaken for an installed hardware IRQ
  vector.
- `Compotune_Digi.sid` showed a concrete bad classification. The payload covers
  high memory, so raw RAM at `$FFFE/$FFFF` contained `$EA77`; the old vector
  selection treated that as a playable hardware IRQ even though `$0001` still
  exposed KERNAL ROM.
- `Kinetix_Blasting_Power_Mix.sid` previously hit a fatal BRK during init.

Changes:

- Added typed CPU/engine failure diagnostics for cycle limits, unsupported
  opcodes, ROM entry, and phase/kind context. This does not change audio by
  itself, but makes compatibility reports actionable.
- Implemented 6510-style BRK vectoring instead of treating opcode `$00` as
  immediately fatal.
- Added a deliberately tiny clean-room KERNAL stub surface:
  - `$EA31` behaves as `RTI` for the engine's current direct IRQ-call model.
  - `$EA34`, `$EA7B`, `$EA7E`, and `$EA81` now also behave as `RTI` for common
    KERNAL IRQ continuation/tail jumps observed in the full HVSC scan.
  - selected KERNAL call entry points such as `CHROUT`, `GETIN`, `LOAD`, and
    channel helpers behave as `RTS` placeholders.
- Made KERNAL stub/ROM visibility depend on `$0001`, so loaded RAM under
  `$E000-$FFFF` is not incorrectly executed when KERNAL ROM is visible.
- Changed IRQ vector discovery to read the mapped hardware vector at
  `$FFFE/$FFFF` while still using `$0314/$0315` as the KERNAL RAM IRQ vector.

Expected effect:

- BRK no longer creates an immediate compatibility-report bucket on its own.
  If a tune was on a wrong path, it should now reclassify into a more precise
  cycle-limit, no-vector, or ROM-entry bucket.
- Handlers that end with `JMP $EA31`, `$EA34`, `$EA7B`, `$EA7E`, or `$EA81` can
  return through the current direct IRQ model without falling into empty ROM.
- Raw payload bytes under KERNAL ROM no longer get mistaken for hardware IRQ
  vectors. In the smoke check, `Compotune_Digi.sid` moved from an unsupported
  opcode at `$EA77` to an init-cycle-limit diagnosis.

Remaining uncertainty:

- These KERNAL stubs are pragmatic compatibility scaffolding, not full KERNAL
  emulation. They are intentionally marked as approximations.
- The current IRQ path is still a direct handler call, not a full
  `$FFFE -> KERNAL prologue -> $0314` interrupt flow.
- Unknown KERNAL ROM entry points still report `rom_entry`; they should drive
  future work items rather than being guessed as valid opcodes.
- BASIC and full RSID startup still need a scheduler with VIC/CIA progression
  before many init-cycle-limit and no-vector rows can be resolved.

### Minimal VIC/CIA register progression

Problem:

- Several HVSC failures were not CPU opcode problems. They were C64 program
  loops polling static I/O bytes such as `$D012`, `$D019`, `$DC04/$DC05`, or
  `$DC0D`.
- `Last_Ninja_1_Remix.sid`, `Demodesigner_II.sid`, and
  `Digidrum_Concert.sid` were representative runtime failures where IRQ/play
  code could spend the whole frame budget waiting for raster or CIA state that
  never changed.

Changes:

- Added a small bus-owned hardware clock that advances after every successful
  CPU instruction and during unused frame idle time.
- `$D011/$D012` now expose an approximate PAL/NTSC raster line, and `$D019`
  supports raster IRQ flag reads plus write-one-to-clear acknowledgement.
- `$D01A` enables the raster IRQ summary bit in `$D019`.
- CIA1 and CIA2 Timer A expose latch writes, running counter reads,
  start/force-load/one-shot control, ICR mask writes, underflow flags, and
  read-to-clear behavior for `$DC0D/$DD0D`.
- CIA speed detection still reads the raw `$DC04/$DC05` latch bytes so existing
  PSID CIA-speed behavior does not depend on the currently running counter.

Expected effect:

- Simple raster and CIA wait loops can make progress without a full C64
  scheduler.
- A targeted 10-file smoke moved `Last_Ninja_1_Remix.sid`,
  `Demodesigner_II.sid`, and `Digidrum_Concert.sid` out of the failure report.
  `Beach_Head_II.sid` subtune 1 also stayed playable in that smoke.
- Startup-heavy files such as `Zimmermanns_White_Keys.sid`,
  `Mickeys_Space_Adventure.sid`, `Random_Ninja.sid`, `Cybertron_Mission.sid`,
  and `Abyss_Zone_Demo.sid` still fail as `init_cycle_limit` or
  `no_irq_vector`; these now point more clearly at BASIC/full-RSID startup
  rather than purely static I/O registers.

Remaining uncertainty:

- This is a compatibility approximation, not cycle-exact VIC-II/CIA emulation.
  Register changes are applied per instruction, not at individual bus-cycle
  positions.
- CIA Timer B, TOD clocks, serial ports, keyboard matrix scanning, VIC badlines,
  sprite IRQs, CIA-delivered CPU IRQ/NMI, and the full KERNAL IRQ prologue are
  still outside this step.
- Some failures intentionally move to more precise buckets. Earlier smoke rows
  that reached unsupported KERNAL ROM around `$EA81` are now covered by the
  minimal KERNAL IRQ-tail stub set; remaining KERNAL entries still require
  per-address evidence before adding more stubs.

### RSID continuous init main-loop fallback

Problem:

- Some RSID files have `play=$0000` and use the init address as a program entry
  point. The code never returns as a PSID-style subroutine, but it already
  writes SID registers and then stays in its own main loop.
- Representative failures included `Impeach.sid`, `Dinner_for_2.sid`, and
  `Access_Denied_Remix.sid`, which were previously reported as init cycle
  limits even though init had made audible SID progress.

Changes:

- During non-BASIC init, the engine counts SID writes while running the bounded
  init subroutine.
- If init hits the cycle limit with `play=$0000`, no usable IRQ vector, and at
  least one SID write, the stream is accepted as a continuous main-loop tune
  instead of failing init.
- Continuous playback advances the CPU for one video frame per rendered frame,
  uses delayed SID writes like the normal path, and reports later CPU or ROM
  failures as play-phase diagnostics.

Expected effect:

- Full-program RSIDs that produce SID output from init and then continue in a
  main loop can render instead of being rejected as stuck init routines.
- A targeted continuous-init smoke moved `Impeach.sid`, `Dinner_for_2.sid`, and
  `Access_Denied_Remix.sid` out of the failure report. `Aztec_Beat.sid` still
  fails later at unsupported KERNAL entry `$FCE2`, which is a more precise
  diagnosis than the old init cycle limit.

Remaining uncertainty:

- This is a guarded compatibility fallback, not a full C64 scheduler. It only
  activates after SID-write progress and does not yet deliver real VIC/CIA IRQs
  while the continuous loop runs.
- Tunes with no SID writes before their first wait loop still fail as init
  cycle limits until the startup scheduler has better progress criteria.

### Init-only SID playback fallback

Problem:

- After filtering out the `Electronic Speech Systems` cluster, the largest
  ordinary HVSC bucket was `no_irq_vector`: 359 failing subtunes across 119
  files.
- Many representatives are game or sound-effect RSIDs with `play=$0000`. Their
  init routine returns without installing an IRQ vector, but it does write SID
  registers. In that shape the SID can keep sounding from its oscillator and
  envelope state without a per-frame play call.
- Representative files included `Lollipops.sid`, `Skull_Island.sid`,
  `Bouncy_Balls_speech.sid`, and several game SFX sets.

Changes:

- The non-BASIC init path now keeps the existing SID-write count after a
  successful init return.
- If `play=$0000`, no usable IRQ vector is installed, and init wrote at least
  one SID register, the stream is accepted as init-only playback.
- Init-only playback advances audio and the minimal hardware clock each frame
  without calling CPU play or IRQ code.

Expected effect:

- Valid one-shot or held-note SFX subtunes no longer fail purely because they
  have no play routine.
- The targeted old `no_irq_vector` fixture set moved from 359 failure rows to
  49 rows after this change.

Remaining uncertainty:

- This is a compatibility fallback, not proof that every accepted tune is
  musically complete for long durations. Some game effects may need future
  host-side retriggering or event sequencing to match the original game.
- Tunes that return without SID writes and without a vector still fail as
  `no_irq_vector`, which is intentional.

### KERNAL IRQ-tail exits from direct play calls

Problem:

- `Wild_Bunch.sid` had 43 failing subtunes. Its direct PSID play address enters
  code that exits through the common KERNAL IRQ tail at `$EA31`, but the engine
  was running it as a synthetic subroutine and waited for an `RTS`.
- The existing KERNAL IRQ-tail stubs already handled IRQ calls, but direct
  play/subroutine calls could still spin until `play_cycle_limit`.

Changes:

- Direct subroutine execution now treats unloaded known KERNAL IRQ-tail
  addresses (`$EA31`, `$EA34`, `$EA7B`, `$EA7E`, `$EA81`) as a valid end of
  the synthetic call.
- The guard only applies when those addresses are not loaded tune RAM, so a
  tune that deliberately provides code there can still execute it.

Expected effect:

- Direct-play tunes that use a KERNAL IRQ-tail exit can complete the frame
  instead of reporting a cycle limit.
- `Wild_Bunch.sid` now renders all 45 subtunes in the 1 second smoke with zero
  failures. The old play-cycle fixture set dropped from 163 to 121 failure rows.

Remaining uncertainty:

- This is still a direct-call compatibility model, not a full KERNAL interrupt
  flow. It deliberately recognizes only observed IRQ-tail exits and does not
  imply broad KERNAL ROM execution.

### 6510 KIL/JAM CPU halt handling

Problem:

- After BASIC, init-only, and KERNAL-tail fixes, the largest remaining ordinary
  HVSC bucket was `unsupported_opcode`: 135 failing rows across 116 files in
  the no-ESS full scan.
- Every opcode in that bucket was from the 6510 KIL/JAM family
  (`$02/$12/$22/$32/$42/$52/$62/$72/$92/$B2/$D2/$F2`).
- These opcodes are not useful player instructions. On real 6510 hardware they
  halt the CPU, while the SID keeps sounding from its current register state.

Changes:

- The CPU now recognizes the KIL/JAM opcode family, records a halted state, and
  reports a typed `CPUHaltError` instead of a generic unsupported opcode.
- The stream renderer treats CPU halt during init, direct play, IRQ play,
  continuous playback, and BASIC-launched machine code as a transition to idle
  playback.
- Idle playback advances audio and the minimal hardware clock without executing
  more CPU instructions.

Expected effect:

- Tunes that intentionally or accidentally reach KIL/JAM no longer fail just
  because the CPU stopped. Existing SID oscillator/envelope state continues to
  render.
- The old unsupported-opcode targeted set moved from 135 failure rows to 2
  follow-up rows (`rom_entry` and `play_cycle_limit`).
- The 1 second all-subtune ordinary-engine scan with `Electronic Speech
  Systems` excluded dropped from 404 failures across 347 files to 269 failures
  across 233 files. `unsupported_opcode` is no longer present in that report.

Remaining uncertainty:

- CPU halt is correct 6510 behavior, but it does not prove every accepted tune
  is musically complete. A stricter silence scan can still flag halted tunes
  that never produced useful SID output.
- This intentionally does not emulate KIL/JAM as normal instructions or guess a
  recovery path. A halted CPU stays halted until a future reset-style engine
  path explicitly decides otherwise.

### KERNAL IRQ hooks and progress overrun frames

Problem:

- After KIL/JAM handling, the largest ordinary no-ESS runtime bucket was
  `play_cycle_limit`: 118 failing rows across 97 files.
- A few failures were KERNAL-tail exits reached with KERNAL ROM hidden, but the
  larger pattern was active IRQ/play code that exceeded the synthetic
  `cyclesPerFrame * 2` budget while making SID-register progress.
- Many RSIDs install `$0314/$0315`, which is the KERNAL RAM IRQ hook. On a real
  C64, the KERNAL IRQ path calls that hook like a subroutine and the hook often
  returns with `RTS`; treating it only as a raw hardware IRQ with `RTI` return
  semantics is too narrow.

Changes:

- Added a CPU helper for KERNAL IRQ hooks. It calls the selected `$0314/$0315`
  handler as a synthetic subroutine, accepts `RTS`, accepts known KERNAL IRQ
  tail addresses, and also accepts `RTI` for handlers that were written for the
  engine's older direct IRQ model.
- Hardware IRQ vectors still use the existing `RunIRQ` path.
- Normal and debug render paths now count SID writes during each runtime
  play/IRQ frame. If the frame hits a cycle limit after at least one SID write,
  the renderer accepts it as a progress-making overrun frame instead of
  failing immediately.
- Cycle limits without SID-write progress remain failures.

Expected effect:

- KERNAL-hook handlers that return via `RTS` or a KERNAL tail no longer fail
  solely because the engine expected `RTI`.
- Active digi/speech/game handlers that run longer than the synthetic frame
  budget can still produce audio instead of being stopped by compatibility
  scanning.
- The old play-cycle targeted set moved from 118 failure rows to 32. The
  1 second all-subtune ordinary-engine scan with `Electronic Speech Systems`
  excluded dropped from 269 failures across 233 files to 167 failures across
  148 files.

Remaining uncertainty:

- Accepting SID-writing overruns is a pragmatic compatibility heuristic. It is
  intentionally guarded by SID writes so unchanged tight loops still fail.
- Some accepted handlers may still run at an approximate tempo because they are
  restarted or restored by the existing fast path rather than resumed by a full
  C64 scheduler.
- The remaining 32 play-cycle rows should be traced before adding another broad
  fallback; they are now more likely to be no-progress hardware waits, BRK/ROM
  paths, or very small CIA-frame-budget cases.

### BASIC RSID interpreter path

Problem:

- HVSC contains many RSID files with the C64 BASIC flag. These are real
  tokenized BASIC programs loaded at `$0801`, not 6510 routines that can be
  called through the normal `init/play` contract.
- The previous engine rejected them all as unsupported. That was honest, but it
  made a large and musically interesting part of the corpus unplayable.

Changes:

- BASIC RSID files now branch into a separate clean-room BASIC V2 interpreter
  path instead of executing tokenized BASIC bytes as CPU opcodes.
- The SID parser now preserves raw header load/init/play addresses separately
  from normalized effective addresses, so future RSID/BASIC validation can
  reason about header intent.
- The BASIC path initializes a small C64/BASIC environment: `$0000/$0001`,
  BASIC text/variable pointers, approximate memory top, and the `$030c-$030f`
  `SYS` register mailbox with `$030c=subtune-1`.
- Implemented tokenized BASIC linked-line parsing and an interpreter for common
  music-program statements: `POKE`, `PEEK`, `SYS`, `FOR/NEXT`,
  `DATA/READ/RESTORE`, `RESTORE <line>`, `IF/THEN`, `IF ... GOTO`, `GOTO`,
  `GOSUB/RETURN`, `ON`, `WAIT`, `GET`, `INPUT`, `CLR`, `PRINT`/`PRINT#` as
  output/timing statements, `REM`, `END/STOP`, numeric variables, string
  variables, and numeric/string arrays.
- String support now covers quoted `DATA`, string assignment/concatenation,
  string comparisons in `IF`, and the common BASIC V2 string helpers `CHR$`,
  `STR$`, `LEFT$`, `RIGHT$`, `MID$`, plus numeric `ASC`, `LEN`, and `VAL`.
  Numeric expressions also handle the common math helpers `SGN`, `SQR`, `LOG`,
  `EXP`, `SIN`, `COS`, `TAN`, and `ATN`.
- BASIC variable names now use C64 BASIC's first-two-character identity, so
  names such as `BEGIN`, `BEG`, and `BE` alias correctly.
- The parser can fall back from corrupt linked-line pointers to physically
  adjacent NUL-terminated BASIC lines. This is needed by BASIC SIDs such as
  `Tennis_BASIC.sid` and `Interceptor_Base_BASIC.sid`, whose link high bytes do
  not point at the actual next line in the loaded payload.
- `DEF FN` is stored as a small expression closure, and `FN` calls evaluate it
  with the supplied argument. Multidimensional array references are flattened
  into the map-backed array store. These are compatibility approximations for
  common music-program use rather than a byte-exact BASIC variable table.
- `FOR` loops whose initial value is already past the limit now skip to the
  matching `NEXT`, and `NEXT I,J` advances stacked loops in statement order.
- BASIC `POKE` writes to the same C64 bus as CPU playback, so SID, VIC, CIA,
  and vector writes use the active compatibility model. `SYS` runs machine code
  on the same CPU/bus and exchanges A/X/Y/P through `$030c-$030f`.
- `SYS` machine code now runs as a CPU coroutine. If it returns with `RTS`,
  BASIC resumes after the `SYS`; if it consumes the frame budget, execution is
  suspended and continued in the next rendered frame instead of being reported
  as a cycle-limit failure.
- If a BASIC launcher finishes during a frame after consuming most of that
  frame's budget and leaves an IRQ vector installed, the IRQ handler starts on
  the next frame with the normal play budget. This avoids turning a valid
  launcher into a tiny leftover-budget IRQ failure.
- The BASIC path installs a clean-room CHRGET/CHRGOT-compatible zero-page
  helper and stubs the common BASIC-ROM argument parser entry points used by
  `SYS addr,arg...` helper routines (`CHKCOM`, `GETBYT`/integer parse family).
  It also covers the narrow BASIC-ROM/FAC entries now seen in HVSC BASIC
  loaders: `$A408` memory-availability check, `$A659/$A68E` RUN/CLEAR text
  pointer setup, `$AD8A` numeric expression parsing, `GIVAYF`, `FIN`, FAC
  rounding, `QINT`, `RND`, and `$B7F7` FAC-to-integer conversion. This is
  targeted compatibility scaffolding, not bundled ROM execution.
- `SYS` helpers that jump directly to the BASIC interpreter loop at `$A7AE` can
  hand off to a different, newly decompressed BASIC program when the original
  program is a one-line `SYS` loader and `TXTPTR+1` points at parseable BASIC
  text. The guard is deliberately narrow to avoid restarting ordinary BASIC
  programs that happen to visit `$A7AE`.
- Numeric variable reads can fall back to the C64 BASIC variable table in memory
  (`VARTAB`/`ARYTAB`) for real and integer variables, and integer-array reads
  can use prebuilt C64 BASIC array descriptors. This covers tunes that POKE
  BASIC's variable pointers at a precomputed variable/array blob.
- Two small custom BASIC bridges are modeled because they map directly to SID
  writes: the SySound `SYS S,...` command parser and the Music Expansion-style
  voice commands such as `1V15`, `1@W64`, and `2F#4`. They are intentionally
  command translators, not full extension ROM emulators.
- Visible BASIC ROM at `$A000-$BFFF` is now classified before loaded RAM shadows
  when `$0001` selects ROM. Conversely, `$C000-$CFFF` remains executable RAM
  even when bytes there were generated by BASIC `POKE` statements rather than
  loaded from the SID payload.
- `PRINT` now has screen-memory side effects. It tracks a simple 40x25 cursor,
  applies common cursor/control codes, converts PETSCII to screen codes, and
  writes through the current screen base byte at `$0288`. This supports BASIC
  SIDs that use `POKE 648,212: PRINT ...` to write SID registers through the
  screen editor path.
- BASIC execution advances audio and hardware time with an approximate dynamic
  per-statement budget.
- The first correction raised the fallback BASIC statement budget from `280` to
  roughly `1400` CPU cycles, and the `WAIT` poll step from `32` to `160`
  cycles, after listening found tokenized BASIC SID programs running roughly
  five times too fast. Follow-up listening found this was too much as a global
  floor: many ordinary BASIC tunes became slow while `Retrospectacles_BASIC.sid`
  and `Videobreak_09_BASIC.sid` were still fast.
- BASIC statements now use a deterministic timing estimator instead of a single
  global statement cost. The floor is now lower, roughly `750` cycles, and the
  per-statement cap is wider, roughly `42000` cycles, so simple statements no
  longer inherit the full earlier overcorrection while dense expressions can
  exceed it. It charges for statement length, numeric and string literals,
  variables, arrays, operators, functions, line-search-like branches, and
  statement classes such as `POKE`, `READ`, `FOR/NEXT`,
  `GOTO/GOSUB`, `IF`, `SYS`, `WAIT`, and `PRINT`. Inline `IF ... THEN POKE`,
  `PRINT`, and `SYS` branches execute their branch body when taken, while
  line-target branches charge an approximate BASIC line-search cost. `SYS` still
  adds real 6502 cycles from the helper routine after the BASIC-side call
  overhead.
- The default BASIC timing weights have been retuned from the
  `zmk-vice-sid-timing` microbench/songbench model generated on 2026-04-28
  (`/tmp/zmk-vice-expanded-large-20260428-vPy9xj`, integrated with the earlier
  80-song fit in `/tmp/zmk-vice-large-box-20260428-bg.ziATsr`). The expanded
  run used 28 synthetic BASIC microbenches as VICE priors, then blended them
  with the ratio-gated 80-song BASIC SID fit (`51` usable songs, `21720` fit
  rows, `statement-feature-cost` `R2=0.9745`). The approximation keeps `POKE`
  and SID literal POKE near the prior tuned range, lowers direct `GOTO`,
  no-target `RESTORE`, array access, and string conversion helpers, raises
  number parsing, arithmetic, grouping parentheses, multi-`READ`, `INT`/`RND`,
  and long variable-heavy expressions, and separates grouping parentheses from
  function/array call parentheses. This is calibrated against VICE monitor
  traces, not a cycle-exact C64 BASIC ROM implementation.
- `Videobreak_08_BASIC.sid` exposed a remaining timing miss in compact pure
  BASIC music loops: its player uses `FOR T=0 TO 15:NEXT` as the note delay.
  The initial `NEXT` fix overcorrected by making every `NEXT` very expensive,
  which could slow unrelated tunes that use BASIC delay loops. `NEXT` now keeps
  a smaller loop-control charge while ordinary `POKE`/`READ` work is not reduced
  below the earlier tuned range. This remains a clean-room approximation, not a
  BASIC ROM cycle trace.
- `Retrospectacles_BASIC.sid` exposed the complementary case: the main tempo is
  not an explicit delay loop, but dense BASIC expression work (`RND`, `INT`,
  `AND`, multidimensional arrays, and array-backed SID frequency `POKE`s).
  Function and logical-operator weights stay high enough for algorithmic
  players, but array access is no longer charged with the earlier broad
  overcorrection. `Videobreak_09_BASIC.sid` motivated this split because it has
  no explicit note delay; its tempo is mostly the repeated BASIC cost of
  `READ`, `INT`, `POKE`, and `GOTO`, and the expanded priors showed direct
  `GOTO` itself should be cheap while surrounding expression and data work
  carries most of the delay. This is also a tuned approximation; it should
  eventually be calibrated with a BASIC-ROM-capable reference run or real
  hardware recording.
- This is still a tuned clean-room approximation, not a measured BASIC ROM cycle
  trace. It is intended to remove the worst one-size-fits-all timing error
  while keeping the browser/WASM build ROM-free.

Expected effect:

- BASIC-RSID files are no longer rejected categorically.
- A BASIC smoke over 587 HVSC BASIC-RSID files and all 1,388 subtunes now has
  zero start/render failures at 50 ms per subtune.
- A 10 second default-subtune silence smoke with `-min-rms 0.0005` leaves 1
  file below threshold. Several earlier 1 second failures were just long BASIC
  startup/intro sequences; `Christmas_Tree_BASIC`, `Swan_BASIC`, `Yesterday`,
  `Tennis_BASIC`, `Interceptor_Base_BASIC`, `Lernaia_BASIC`, `Ede_BASIC`, and
  both `Toonypoo` fixtures now produce audio in longer checks.
- The `Randomly_generated_music_BASIC` fixtures now pass the 10 second silence
  threshold after the clean-room FAC/number helper stubs. `SySound_BASIC`,
  `Music_Expansion_Demo_BASIC`, `Dance_into_the_Groove_BASIC`, and
  `Entertainer_BASIC` now pass after the custom SYS/voice-command bridges,
  `$AD8A`/`$B7F7` parser correction, and memory-backed variable/array reads.
- `Allt_Som_Jag_BASIC` now passes because the initial BASIC environment uses
  the loaded payload end for `VARTAB`, `ARYTAB`, and `STREND`, not only the
  visible first BASIC program end. This matches loaders that hide copy/decode
  data after a one-line BASIC `SYS` stub.
- `Black_Box_V8_Demo_BASIC` now passes because its Sound Master/SAM installer is
  recognized, the installer `SYS 7168` is treated as an extension setup
  handoff, and the Sound Master music commands (`VOLUME`, `WAVE`, `ENVELOPE`,
  `OSCILLATE`, `TUNE`, `PLAY`, `FILTER`, `SOUNDCLEAR`) are mapped to SID
  register writes. The speech/SAM commands are still skipped rather than
  synthesized.
- `Back_to_Basics_256_bytes_BASIC.sid` specifically validates the new behavior:
  its single BASIC line enters a non-returning `SYS` loop that waits on `$D012`
  and updates SID registers frame by frame.
- The full 1 second all-subtune HVSC scan now has zero BASIC rows in the
  failure report. `Randomly_generated_music_2_BASIC.sid` subtunes 139 and 170
  previously failed after the BASIC program installed an IRQ handler with only a
  few dozen cycles left in the current frame; a targeted 256-subtune recheck now
  reports zero failures.
- Pure BASIC music loops now execute far fewer statements per video frame. This
  should reduce the clearly too-fast playback heard on BASIC SIDs, especially
  tunes that drive notes directly through `POKE`, `FOR/NEXT`, `READ`, and
  `GOTO` rather than handing timing to machine-code `SYS` routines.
- Different BASIC styles should now separate more naturally: compact direct
  `POKE` loops, `READ`/`DATA` players, string-heavy loaders, array-driven
  players, `FOR/NEXT` delay loops, expression-heavy algorithmic players, and
  `SYS` helpers no longer all consume the same frame budget.

Remaining uncertainty:

- This is partial BASIC V2 compatibility, not a full C64 BASIC ROM. String
  behavior now covers common music-program usage, but not every C64 BASIC
  corner case or memory-layout side effect. `PRINT#`, file I/O, and prompt/input
  behavior remain mostly timing/no-op behavior.
- BASIC timing is approximate. The estimator constants are listener-facing
  corrections for obviously fast BASIC playback, but they have not been
  calibrated against real BASIC interpreter cycle costs or reference recordings
  across the BASIC corpus.
- `SYS` main loops can now continue across frames, but true interrupt scheduling
  while such a loop is running is still not implemented.
- Deferring a just-installed BASIC IRQ to the next frame is an approximate
  scheduling choice. It is preferable to running with an arbitrary leftover
  budget, but it is not cycle-exact C64 interrupt timing.
- Sound Master support is a compatibility bridge for BASIC-scripted SID music,
  not a full reproduction of the original extension runtime. `TUNE`/`PLAY`
  currently establish a representative SID voice from the scripted note string
  instead of scheduling every note duration.
- A `SYS` path that enters an unsupported BASIC-ROM routine is still treated as
  a handoff/return point unless one of the explicit parser stubs handles it.
- The remaining 10 second silence list is now down to `Beat_Dis_BASIC`, an
  interactive "C64 Speech System V2.7" program with no deterministic scripted
  playback command in the SID file. `Black_Box_V8_Demo_BASIC` still contains
  `SAY`/control speech commands similar in shape to Magic Voice-style BASIC
  extensions (`SAY`, `RATE`, `VOC`, `RDY`), whose audio may be external to SID
  registers: https://www.c64-wiki.de/wiki/Magic_Voice.
- ROM-backed BASIC/KERNAL execution remains intentionally out of scope because
  bundling original ROM images creates licensing and web-distribution problems.

## What Is Deliberately Approximate

- Full C64 scheduling is not implemented.
- VIC timing, raster IRQ behavior, CIA behavior, ROM startup paths, and complete
  RSID environment behavior are still limited.
- BASIC RSID support is a clean-room compatibility interpreter for common music
  programs, not complete Commodore BASIC V2 emulation.
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
go run ./cmd/zmk-sid analyze -duration 5s ../test_tunes/C64Music/MUSICIANS/F/Freqvibez/Cordis_Monophonous.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/C64Music/MUSICIANS/H/Honey/8_Bit-Maerchenland_V2.sid
go run ./cmd/zmk-sid analyze -duration 10s ../test_tunes/C64Music/MUSICIANS/H/Honey/Alles_Luege.sid
go run ./cmd/zmk-sid analyze -duration 8s ../test_tunes/C64Music/MUSICIANS/S/SoedeSoft/Soede_Jeroen/Airwolf_Title.sid
go run ./cmd/zmk-sid duration -budget 3s ../test_tunes/C64Music/MUSICIANS/H/Honey/Alles_Luege.sid
go run ./cmd/zmk-sid duration-validate -songlengths ../test_tunes/C64Music/DOCUMENTS/Songlengths.md5 ../test_tunes/C64Music/MUSICIANS/H/Hubbard_Rob
go run ./cmd/hvsc-compat -duration 1s -subtunes all -workers 12 -out /tmp/zmk-sid-full-hvsc-1s-after-basic-irq.tsv ../test_tunes/C64Music
go run ./cmd/hvsc-compat -duration 1s -subtunes all -workers 12 -exclude-type "Electronic Speech Systems" -out /tmp/zmk-sid-full-hvsc-1s-no-ess-after-topfixes.tsv ../test_tunes/C64Music
go run ./cmd/hvsc-compat -duration 1s -subtunes all -workers 1 -list /tmp/zmk-sid-basic-recheck.txt -out /tmp/zmk-sid-basic-recheck.tsv ../test_tunes/C64Music
(cd ../zmk-optimize && go run ./cmd/recording-compare -sid ../test_tunes/C64Music/MUSICIANS/S/SoedeSoft/Soede_Jeroen/Airwolf_Title.sid -ref ../test-results/recording-compare/soasc-airwolf-title/Airwolf_Title_T001.sid_MOS6581R3.mp3 -duration 30s -max-lag 8s -o ../test-results/recording-compare/soasc-airwolf-title/airwolf-title-r3-30s.json)
```

Useful current problem tunes:

- `Cordis_Monophonous.sid`: click and note-edge behavior.
- `8_Bit-Maerchenland_V2.sid`: subtle `pulse+saw` track around 7-8 seconds.
- `Alles_Luege.sid`: CIA speed and perceived timing.
- `Airwolf_Title.sid`: max-resonance 6581 low-pass sweep on voice 3; catches
  over-aggressive filter ringing and crackle-like transients.

Metrics are not enough on their own. Peak, RMS, max delta, and zero crossings can
catch regressions, but listening tests and reference renders are still required.

## Profile Support

2026-04-27:

- Added a versioned SID sound profile format in `profile/`.
- The current built-in behavior is exposed as the default `balanced` profile.
- `RenderOptions`, `StreamOptions`, and `DebugOptions` can now carry a
  `SoundProfile`.
- `zmk-sid play`, `zmk-sid render`, and `zmk-sid analyze` accept
  `-profile <name-or-json>`.
- Supported profile areas currently include mixer gain/leakage/drive, waveform
  color such as 6581 triangle saw bleed, voice DAC low-pass, cutoff mapping,
  filter input/drive/damping/mode response, output stage, output gain, and
  volume DAC level.
- `filter.cutoff.points` can now optionally provide sorted raw-register/Hz
  points. When at least two points are present, zmk-sid linearly interpolates
  those points instead of using the polynomial cutoff
  `baseHz + rangeHz * dac(raw)^exponent` curve. This is intentionally explicit
  and profile-driven; the built-in `balanced` defaults still use the polynomial
  mapping.
- This is a structural change, not a new retune. The default constants were
  moved behind a resolved `balanced` profile so `zmk-optimize` can emit
  candidate profiles without requiring immediate Go-code edits.

## 6581 Balanced Profile Promotion

2026-04-27:

- Promoted the best no-prune `zmk-optimize global-optimize` result into the
  built-in 6581 `balanced` defaults. The source candidate was
  `test-results/optimize/global-cma-filterv2-noprune-env-002/profiles/best-profile.json`,
  generated by the filter-focused v2 CMA pipeline with all candidates fully
  evaluated against synthetic sweeps and the cached SOASC real-recording corpus.
- What we had before: the 6581 defaults were hand-tuned constants from the
  Airwolf/filter investigation. They already fixed the worst Airwolf filter
  artifacts, but the cutoff curve, drive, per-mode response, and output color
  were still mostly manual approximations.
- What changed: the default 6581 cutoff base/range/exponent, filter drive,
  feedback drive, damping, per-mode LP/BP/HP response curves, filter output
  gain, mixer leakage/output gain, and output-stage drive/asymmetry now match
  the promoted profile. 8580 defaults are unchanged.
- Why: the no-prune optimizer improved the total benchmark loss from `8.1477`
  to `6.7510` without synthetic pruning. Phase contributions were:
  `curve 8.1477 -> 7.1850`, `drive 7.1054 -> 6.7917`, and
  `full 6.7698 -> 6.7510`.
- Expected improvement: the synthetic sweep fit improved substantially:
  curve-after RMSE `0.557 dB -> 0.226 dB`, and fit-test RMSE
  `0.793 dB -> 0.637 dB`. Airwolf guardrail values in the final full phase
  improved from spectral `0.9707 -> 0.9538` and band `0.5446 -> 0.5324`.
- Remaining uncertainty: real-recording aggregate movement is mixed, not a
  clean win on every metric. Median real filter band improved
  `0.635 -> 0.564`, while median real filter spectral moved
  `0.942 -> 0.954`. This is therefore still a tuned approximation, not a
  measured 6581 hardware profile.
- Validation status: the promoted candidate passed the configured recording
  guardrails, including 10/10 recording comparisons, 3/3 filter-window
  comparisons, full selected-filter coverage, and the Airwolf tune guardrails.

Promotion uncertainty:

- The promoted constants are coupled to the current filter model and benchmark
  weights. Future work should keep a holdout corpus, compare additional
  problem tunes, and eventually validate against measured or trusted hardware
  captures before calling this a hardware-accurate 6581 profile.
- `zmk-optimize` now freezes this promoted default as
  `balanced-6581-filterv2` and provides `compare-profile` as the next promotion
  gate: current vs candidate, synthetic and real metrics, Airwolf/problem-tune
  rows, guardrails, score-component deltas, and optional holdout must agree
  before another profile should replace the built-in default.
- The optional cutoff point curve is a model-extension hook for future
  optimizer runs. It is a controlled way to fit the analog-looking curve shape,
  but any point-based profile remains a tuned approximation until validated
  against reference renders and, ideally, measured hardware captures.

Profile-format uncertainty:

- The format is intentionally small and interpretable. It is not yet a complete
  description of every SID behavior detail.
- Candidate profiles are still tuned approximations unless their provenance
  points to measured sweeps, reference renders, or hardware recordings.
- Bundled non-balanced profiles should only be added after Airwolf-style
  peak/max-delta guardrails and sweep comparisons pass.

## zmk-optimize Migration

2026-04-27:

- Created `../zmk-optimize` as the new home for analysis and optimization
  tooling.
- Migrated the current standalone analysis commands there:
  `filter-sweep`, `filter-sweep-report`, `filter-curve-align`,
  `filter-response-fit`, `recording-compare`, `soasc-corpus`, and
  `waveform-harmonics`.
- Added an initial `zmk-optimize profile validate` command.
- Removed those analysis command directories from `zmk-sid`; run them from
  `../zmk-optimize/cmd/...` now. Historical notes below may still mention their
  original `cmd/...` paths.

## BASIC Runner Completeness

2026-04-27:

- Completed the helper pieces for the in-repo BASIC runner so `zmk-sid` builds
  again during `zmk-optimize` recording comparisons.
- The immediate blocker appeared during the `LP+HP` pairwise filter run: the
  renderer failed to compile `internal/basic` before it could compare candidate
  audio against SOASC recordings.
- The change fills in DATA item metadata, numeric/string READ storage,
  string-expression parsing helpers, string comparisons in IF, and FOR-loop
  entry/skip helpers. These are established BASIC interpreter semantics in
  broad shape, but the implementation remains an approximation aimed at common
  SID tune launchers rather than a complete C64 BASIC V2 clone.
- A follow-up `zmk-optimize` hybrid curve smoke exercised this path from the
  external optimizer module. The helper set now includes `looksLikeBasicLine`
  for bad BASIC linked-list pointers with valid physical next lines, and
  `statementEnd` so `DEF FN` captures only its expression before the next
  statement separator. Both are compatibility helpers, not new measured SID
  behavior.
- HVSC BASIC follow-up work added silence diagnostics plus narrow BASIC-ROM/FAC
  helpers for common `SYS` machine-code launchers (`$A408`, `$A659/$A68E`,
  `$AD8A`, `GIVAYF`, `FIN`, FAC rounding, `QINT`, `RND`, and `$B7F7` FAC
  integer conversion). It also added C64 BASIC memory-backed variable/array
  reads, the narrow `$A7AE` one-line-loader handoff, and custom command bridges
  for SySound and Music Expansion-style voice commands. These reduced the
  10 second BASIC silence list from 9 files to 3.
- Expected improvement: BASIC-bootstrapped tunes and generated comparison runs
  can render again without falling back to failed candidate evaluations.
- Remaining uncertainty: edge cases around multi-dimensional arrays, exact
  string formatting, INPUT behavior, and full C64 BASIC error semantics still
  need targeted compatibility tests before relying on them for arbitrary BASIC
  programs.

### Stream fast-forward for playback start offsets

- `zmk-sid play -start` previously skipped by reading PCM from the stream into a
  scratch buffer and discarding it. That was correct, but it paid the normal PCM
  output cost for audio that would never be heard.
- `Stream` and `DebugStream` now expose `SkipSamples`, and the playback helper
  uses that fast path when a source provides it. Whole frames are advanced with
  a discard audio clock, so CPU, bus, SID oscillator/envelope/filter state,
  delayed SID writes, CIA-speed refresh, BASIC playback, continuous playback,
  idle playback, snapshots, and configured debug traces still move through the
  normal engine path.
- Added an explicit approximate `FastForwardSamples` path for interactive
  seeking. It keeps CPU execution, RAM mutation, hardware timer progression,
  SID register writes, frame scheduling, BASIC, continuous, and idle playback
  on the normal code path, but advances SID oscillator/envelope state with a
  cheap coarse update instead of rendering every discarded SID sub-sample.
  `DebugStream.FastForwardSamples` temporarily suppresses trace hooks while
  seeking because UI seeks do not need instruction/audio/debug events.
- The fast path intentionally does not translate seconds into a player-specific
  song position. It remains simulation-based seeking; it just avoids
  materializing PCM for whole skipped frames. The final partial frame still uses
  ordinary reading/discarding so sample alignment and pending overflow samples
  stay exact.
- Expected improvement: `play -start 45s` and similar offsets spend less work
  on discarded output while preserving the samples heard after the skip.
- Remaining uncertainty: the expensive SID sample-state update still has to run
  for exact generic seeking. The turbo path trades exact analog/SID settling
  during the skipped span for responsiveness, so the first samples after a seek
  can differ from a fully rendered skip. A later snapshot index would be needed
  for sublinear repeated exact seeks.

## Recommended Next Steps

1. Expand explicit sound profiles.

   `balanced` now exists as the default profile behavior. Next candidates:

   - `accurate`: fewer playback-polish ramps, stricter SID semantics, and either
     reference-engine comparison or a future reSID-style filter model.
   - `6581r3-experimental`: candidate generated by `zmk-optimize` from sweeps
     and guarded by Airwolf Title.
   - `8580r5-experimental`: same idea for 8580-focused material.
   - `soft`: stronger de-clicking for casual playback.

2. Extend the reference comparison harness.

   `../zmk-optimize/cmd/filter-sweep`,
   `../zmk-optimize/cmd/filter-sweep-report`, and
   `../zmk-optimize/cmd/recording-compare` now cover controlled synthetic
   stimuli, sidplayfp/reSIDfp comparisons, and real-recording validation. Use
   those harnesses before changing more filter constants:

   - inspect per-mode/per-resonance cutoff curves in the HTML report
   - make multi-wave sweeps robust enough for full pulse and triangle runs
   - compare dry-normalized sweeps with optional `sidplayfp -nf` reference runs
   - build a small real-recording corpus outside the repo, starting with SOASC
     entries that exactly match local test tunes by title, composer, path,
     subtune, clock, and SID model
   - keep MP3/FLAC decode, alignment quality, chip revision, and recording
     source visible in every real-reference report
   - add a fitter that scores candidate filter constants against the sweep
     curves while keeping Airwolf Title as both an artifact guardrail and a
     real-6581R3 color guardrail
   - optionally keep reference WAV snippets in a separate test-data repo

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
