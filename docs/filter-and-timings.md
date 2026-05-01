# Filter and BASIC Timing Calibration

This document explains how the current filter profile and BASIC timing defaults
were chosen. Both areas are calibrated approximations: they are intended to make
common SID material sound and run plausibly while keeping the engine small,
pure Go, and ROM-free.

## Scope

The calibration work focused on two audible failure modes:

- Filtered 6581 tunes could sound too dark, too loud in the wrong bands, or
  numerically unstable at high resonance and moving cutoff values.
- BASIC RSID tunes could run far too fast or too slow when every BASIC
  statement was charged a single fixed cycle cost.

Neither result should be read as measured chip or ROM behavior. The filter is a
tuned model fitted against reference renders and recordings. The BASIC runner is
a clean-room timing estimator fitted against traced workloads, not an embedded
C64 BASIC ROM.

## Filter Calibration

The filter work started with problem-tune listening and then moved to controlled
measurement. A high-resonance 6581 low-pass passage showed that the earlier
explicit-Euler state-variable filter could ring and produce large sample deltas.
The filter integrator was replaced with a topology-preserving state-variable
form so fast cutoff movement and high resonance keep continuous state instead of
turning into uncontrolled ringing.

After that structural fix, the sound profile was calibrated in stages:

1. Generate small synthetic SID files with known stimuli: one routed voice,
   fixed chip model, fixed waveform, one filter mode, one resonance value, and
   one raw cutoff value.
2. Render the same stimuli through this engine and through a trusted reference
   renderer.
3. Skip the attack and settling window, then compare RMS, peak, spectral
   centroid, normalized frequency-band shares, log spectral distance, and band
   distance.
4. Render a matching dry, unrouted stimulus and divide the filtered result by
   the dry result. This removes most oscillator, mixer, and output-stage level
   differences so the remaining signal describes the filter transfer more
   directly.
5. Fit the cutoff curve, mode response, damping, drive, leakage, and output
   color in separate passes instead of collapsing all errors into one global
   loudness adjustment.
6. Promote a candidate only after it passes synthetic sweeps, real-recording
   comparisons, and problem-tune guardrails.

The 6581 cutoff mapping is currently expressed as a modeled DAC curve:

```text
cutoffHz = baseHz + rangeHz * dac(raw)^exponent
```

The profile can also supply explicit raw-register-to-Hz points; when present,
those points override the polynomial curve. The built-in `balanced` 6581 profile
uses the polynomial form with a small ripple term because it matched the current
test set better while staying compact.

Per-mode response is fitted in decibels over normalized cutoff and resonance:

```text
gainDB = base + cutoff*x + cutoff2*x*x + resonance*r + cutoffRes*x*r
```

where `x` is the raw cutoff value normalized to `0..1` and `r` is the normalized
resonance value. Separate coefficients are used for low-pass, band-pass, and
high-pass output. This lets the profile correct, for example, a band-pass curve
that is too quiet at low cutoff but too loud at high cutoff without pretending
that the whole filter only needs one output gain.

The guardrails deliberately mix narrow and broad checks. Synthetic sweeps are
good at exposing curve shape, resonance behavior, and mode gain errors. Full
tune renders catch interaction with real players, combined waveforms, D418
volume DAC use, voice routing, output filtering, and clipping. Real-hardware
recordings help keep the tuned profile from overfitting to another emulator's
particular output balance.

The promoted 6581 `balanced` profile came from a no-prune candidate search:
candidate profiles were not accepted from a cheap synthetic score alone, but
were also checked against selected real-recording windows and problem-tune
limits. In that pass the combined benchmark loss moved from about `8.15` to
about `6.75`; the controlled sweep curve RMSE moved from about `0.56 dB` to
about `0.23 dB`. Those numbers are useful as regression evidence, not as a
claim that the constants describe one measured SID chip.

## BASIC Timing Calibration

BASIC RSID support does not run a C64 BASIC ROM. Instead, tokenized BASIC lines
are interpreted by a small clean-room runner. To keep music tempo useful, the
runner advances CPU/audio time with a deterministic per-statement cycle
estimate.

The first version used one fixed statement cost. That made many BASIC music
loops roughly coherent, but it could not handle the difference between a short
`NEXT`, a dense expression with `RND` and arrays, a multi-value `READ`, and a
`POKE` that writes SID registers. Listening checks showed the simple global
cost could make one group of tunes better while making another group clearly
wrong.

The current estimator charges for statement structure and expression work:

- a base cost plus a cost per tokenized byte
- numeric literals, string literals, variables, arrays, operators, and common
  functions
- statement classes such as `POKE`, `READ`, `FOR/NEXT`, `GOTO/GOSUB`, `IF`,
  `SYS`, `WAIT`, and `PRINT`
- approximate line-search cost for literal target branches
- extra cost for inline `IF ... THEN` bodies when the branch is taken
- real 6502 helper-routine cycles after the BASIC-side `SYS` overhead

The weights were calibrated from two inputs:

- `28` synthetic BASIC microbenchmarks were run in a reference C64 environment
  with monitor tracing enabled. These gave cycle priors for isolated operations
  such as `POKE`, `READ`, `GOTO`, `FOR/NEXT`, functions, arrays, string
  conversion, and arithmetic-heavy expressions.
- An `80` song BASIC SID songbench was traced through this engine. After ratio
  gating, `51` songs and `21720` statement-feature rows were usable for the
  fit. The resulting statement-feature model reached an `R2` of about `0.9745`
  on that fitted set, then the final weights were blended with the synthetic
  priors so the model did not simply memorize the songbench.

The resulting statement-feature model keeps simple statements near a lower
floor, allows dense statements to cost much more, and caps pathological cases so
the browser/WASM path remains bounded. Current defaults use a statement floor of
about `750` CPU cycles, a statement cap of about `42000` cycles, and a `WAIT`
poll step of about `160` cycles.

Important tuning lessons from the songbench pass:

- Direct `GOTO` itself should stay relatively cheap; the surrounding
  expression, data, and line-search work usually carry more of the tempo.
- `POKE`, especially SID-register `POKE`, must remain expensive enough for
  register-heavy music loops.
- `NEXT` needs loop-control cost, but making every `NEXT` very expensive slows
  unrelated tunes that use ordinary loop structure.
- `INT`, `RND`, logical operators, grouping parentheses, arrays, and
  variable-heavy expressions need enough weight for algorithmic BASIC players.

This is intentionally a clean-room approximation. It removes the worst
one-size-fits-all timing error while preserving the project's ROM-free shape.
Programs that rely on precise BASIC ROM side effects, exact garbage collection
pauses, or full C64 scheduling can still differ from a real machine.

## Validation

The calibration is guarded by a mix of tests and manual checks:

- unit tests for profile override behavior, cutoff point interpolation,
  response-dB terms, and BASIC statement timing shape
- problem-tune renders for note-edge timing, CIA-speed playback, high-resonance
  filter sweeps, and BASIC delay loops
- broad compatibility scans over HVSC-style tune sets
- synthetic filter sweeps with dry-normalized comparisons
- real-recording comparisons for selected 6581 material

The remaining uncertainty is explicit: the current defaults are good tuned
approximations, not proof of hardware-accurate SID filter behavior or
cycle-exact C64 BASIC timing.
