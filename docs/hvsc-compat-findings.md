# HVSC Compatibility Implementation Analysis

This document is a working analysis for improving rasterklang compatibility against
the HVSC smoke-test report produced by `cmd/hvsc-compat`.

Current ordinary-engine snapshot with `Electronic Speech Systems` filtered out:

- Command:
  `go run ./cmd/hvsc-compat -duration 1s -subtunes all -workers 12 -exclude-type "Electronic Speech Systems" -out /tmp/rasterklang-full-hvsc-1s-no-ess-after-progress-overrun.tsv ../test_tunes/C64Music`
- Report: `/tmp/rasterklang-full-hvsc-1s-no-ess-after-progress-overrun.tsv`
- Snapshot time: 2026-04-27
- Corpus after type filter: 86,484 subtunes
- Failure rows: 167
- Distinct files with at least one failing subtune: 148
- Formats in failed rows: 147 RSID, 20 PSID
- BASIC rows in the failure report: 0

The `Electronic Speech Systems` cluster remains deliberately filterable as a
speech-specialized concern. The targeted ESS scan still has 537 failure rows
from 590 subtunes; those rows are now labeled
`RSID,Electronic Speech Systems,Speech extension` instead of being mixed into
ordinary engine buckets.

Rows are failing subtunes, not unique files. `cmd/hvsc-compat` runs all
subtunes by default, so multi-subtune files can dominate row counts.

## Bucket Summary

| Bucket | Failure rows | Distinct files | Format split | Immediate cause / next lever |
| --- | ---: | ---: | --- | --- |
| No IRQ vector | 76 | 70 | RSID only | Remaining `play=0` tunes have no vector and did not qualify for init-only playback; many are now KERNAL-tail or full-program scheduler cases. |
| Play/IRQ cycle limit | 32 | 26 | 20 RSID, 12 PSID | Remaining no-progress long handlers, hardware waits, or very small CIA-frame budgets. Needs trace-driven scheduler/hardware fixes rather than a blanket budget raise. |
| Init cycle limit | 31 | 25 | 23 RSID, 8 PSID | Remaining startup loops without enough progress evidence for the continuous or init-only fallbacks. |
| ROM entry | 28 | 28 | RSID only | Remaining KERNAL/BASIC/I/O entry points such as `$EA87`, `$FEBC`, `$FCE2`, and `$DC0C`. Needs explicit stubs or a future user-ROM mode, not silent execution through empty ROM. |

Current top implementation tracks:

1. Add a real full-RSID startup/main-loop scheduler for `play=0` tunes that do
   not fit the simple `init`/IRQ contract.
2. Expand KERNAL/BASIC/I/O stubs only from observed entry-point evidence.
3. Trace the remaining no-progress long handlers before adding more scheduling
   heuristics.

## Implementation Progress

Done:

- `cmd/hvsc-compat` now writes streaming TSV reports with bucket, phase, tune
  metadata, addressing metadata, Songlengths MD5, and structured engine context
  columns.
- Reports include the parser's filterable `types` labels, for example `RSID`,
  `BASIC`, `SAM/Reciter`, `Magic Voice`, `Electronic Speech Systems`,
  `Sound Master`, or `Speech extension`.
- `cmd/hvsc-compat -min-rms` now records silence diagnostics: normalized RMS,
  RMS floor, SID-write count, first/last SID-write sample time, and a coarse
  `silence_class`.
- `cmd/hvsc-compat -list <file>` can run newline-separated fixture lists
  relative to one HVSC root directory.
- `cmd/hvsc-compat -include-type` and `-exclude-type` can limit scans by the
  parser's filterable type labels. For example,
  `-exclude-type "Electronic Speech Systems"` runs the ordinary engine corpus
  without that speech-specialized cluster.
- CPU `BRK`, CPU halt, unsupported opcode, and cycle-limit failures now carry
  typed error data.
- Engine `NewStream`/render failures now wrap errors as structured
  `FailureError` values without changing playback behavior.
- BRK now vectors instead of being fatal, and the bus exposes a tiny bank-aware
  KERNAL stub surface for common IRQ/KERNAL returns.
- IRQ vector discovery now reads mapped `$fffe/$ffff` rather than raw RAM under
  KERNAL ROM, preventing payload bytes under ROM from being misclassified as
  installed hardware IRQ vectors.
- The bus now has approximate VIC/CIA time progression for common RSID wait
  loops: `$d011/$d012`, `$d019/$d01a`, and CIA1/CIA2 Timer A plus ICR
  read/write behavior.
- A targeted Track-2 smoke over 10 files / 32 subtunes still reported 28
  failures, but the previous runtime cycle-limit representatives
  `Last_Ninja_1_Remix.sid`, `Demodesigner_II.sid`, and `Digidrum_Concert.sid`
  no longer appeared in the failure report. Remaining rows are dominated by
  startup/full-program issues (`no_irq_vector`, `init_cycle_limit`) and KERNAL
  ROM entry around `$EA81`.
- BASIC RSID files now have a separate clean-room BASIC interpreter path instead
  of being rejected by `ValidateForPOC`. The current BASIC smoke covers 587 HVSC
  BASIC files and 1,388 subtunes with zero start/render failures. A stricter
  10 second default-subtune silence check leaves 1 file below the RMS floor.
- BASIC launchers that finish after consuming most of a frame now defer their
  installed IRQ handler to the next frame instead of calling it immediately with
  a tiny leftover budget. This removed the last two BASIC rows from the full
  1 second all-subtune HVSC scan.
- RSID files with `play=$0000` whose init routine never returns but already
  writes SID registers can now continue as frame-driven main loops. This moved
  targeted continuous-init representatives such as `Impeach.sid`,
  `Dinner_for_2.sid`, and `Access_Denied_Remix.sid` out of the init-cycle-limit
  bucket.
- The minimal KERNAL IRQ-tail stub set now covers `$EA31`, `$EA34`, `$EA7B`,
  `$EA7E`, and `$EA81`. A targeted KERNAL-tail smoke over
  `Aces_High_tune_2.sid`, `SX-64_Demo.sid`,
  `Copy-Party_Demo_tune_1.sid`, and `Drink_Twice.sid` produced zero failures.
- Current 1 second all-subtune ordinary-engine scan with
  `Electronic Speech Systems` excluded: 167 failures across 148 files from
  86,484 tested subtunes.
- `Electronic_Speech_Systems` metadata is now promoted to parser-visible
  `Electronic Speech Systems` and `Speech extension` type labels. A targeted
  scan over that HVSC directory preserves the 537 failure rows, but makes the
  rows filterable by type instead of leaving them as plain `RSID`. The same
  targeted scan with `-exclude-type "Electronic Speech Systems"` tests 0
  subtunes and reports 0 failures.
- Init-only RSIDs are now accepted when init returns, `play=$0000`, no IRQ
  vector is installed, and init wrote SID registers. The old no-IRQ fixture set
  moved from 359 failures to 49 failures.
- Direct play/subroutine execution now treats unloaded KERNAL IRQ-tail exits as
  valid synthetic-call returns. `Wild_Bunch.sid` moved from 43 failures to 0;
  the old play-cycle fixture set moved from 163 failures to 121.
- The 6510 KIL/JAM opcode family (`$02/$12/.../$F2`) now halts the CPU instead
  of being reported as an unsupported opcode. Rendering then continues in idle
  playback with the current SID register state. The old unsupported-opcode
  targeted set moved from 135 failure rows to 2 follow-up rows
  (`rom_entry`, `play_cycle_limit`), and the ordinary no-ESS full scan dropped
  from 404 to 269 failures.
- KERNAL RAM IRQ vectors (`$0314/$0315`) now run as KERNAL IRQ hooks: RTS
  returns to the synthetic KERNAL continuation, while RTI-style handlers remain
  accepted for compatibility. Runtime play/IRQ cycle limits that made SID-write
  progress are accepted as overrun frames instead of failing immediately. The
  old play-cycle fixture set moved from 118 failure rows to 32, and the
  ordinary no-ESS full scan dropped from 269 to 167 failures.

Still open for Track 0:

- Add optional bounded PC/write histograms for individual failing tunes.
- Add richer failure context for startup loops that do not yet produce a typed
  CPU error.

## Current Engine Shape

Relevant current behavior:

- `internal/sidfile/sidfile.go` marks BASIC RSID through `Tune.Basic`, and the
  engine routes those files into a separate BASIC interpreter path.
- `internal/engine/render.go` loads the SID payload, creates a CPU, then first
  tries the fast `cpu.RunSubroutine(init, subtune-1, tune.CPUClockHz()*2)` init
  path.
- Init is considered successful if that synthetic subroutine returns, if it
  hits the cycle limit after installing an IRQ vector and `play=0`, or if it
  hits the cycle limit after making SID progress and can continue as a
  full-program main loop.
- For playback, direct-play tunes call `RunSubroutineWithHook(play, ...)`.
- Interrupt-driven tunes call `RunIRQWithHook(vector, ...)` once per rendered
  frame.
- If the selected vector is the KERNAL RAM IRQ hook at `$0314/$0315`, the
  engine calls it as a KERNAL subroutine hook and accepts RTS/RTI/KERNAL-tail
  exits.
- Runtime play/IRQ handlers that exceed the synthetic budget after SID writes
  are accepted as progress-making overrun frames.
- Hardware state is still minimal, but `$d012`, `$d019`, `$d01a`,
  `$dc04/$dc05`, `$dc0d/$dc0e`, and their CIA2 Timer A counterparts now have
  enough active behavior for simple raster/CIA waits.
- Default IRQ vectors are seeded to `$EA31`, and selected clean-room KERNAL
  return/stub entry points are visible when `$0001` exposes KERNAL ROM.
- CPU `BRK` vectors through the mapped IRQ/BRK vector instead of being fatal.
- CPU KIL/JAM opcodes halt CPU execution and leave the renderer in idle
  playback instead of becoming unsupported-opcode failures.

This works for many PSID-style players and now covers BASIC-RSID launchers, but
it is still too small for full RSID behavior. The failing buckets are mostly
consequences of treating real C64 programs as simple `init/play` subroutines.

## Track 0: Reporting And Diagnostics

This is not a compatibility feature by itself, but it was the first useful
implementation step. The scanner now writes stable buckets, tune metadata,
type labels, runtime context, memory classification, vector diagnostics,
SID-write counts, and optional silence diagnostics.

Still useful additions to `cmd/hvsc-compat`:

- Optional `-trace-failure` mode for a small bounded PC histogram and last N
  writes to IRQ/VIC/CIA/SID registers.
- Better loop signatures for startup failures, especially repeated PCs with no
  SID writes versus active main loops that are merely missing a return/vector.
- More external/speech-specialized fingerprints beyond the current
  `Electronic_Speech_Systems`, Magic Voice, SAM/Reciter, and C64 Speech System
  labels.

Why it matters:

- The current error string groups many different causes together. For example,
  `init failed: ... exceeded cycles` can mean a valid main loop, a raster wait,
  missing KERNAL behavior, a bad vector, or a true infinite loop.
- The next agents should be able to pick a bucket and see whether a change
  reduced it or merely moved rows to a different bucket.

Implementation notes:

- Keep diagnostics behind options or only in failure paths. Normal streaming
  should not allocate trace objects.
- Most of this can reuse ideas from `internal/engine/debug.go`, but the scanner
  needs summary data even when `NewStream` fails during init.

## Track 1: BASIC RSID Mode

Snapshot evidence:

- 1,388 failing rows.
- 587 distinct files.
- All sampled files have `load=$0801`, `init=$0801`, `play=$0000`, and the RSID
  BASIC flag set.
- Only 4 files are pure one-token `SYS` programs.
- Only 7 files look like tiny one- or two-line `SYS` launchers under 64 bytes.
- 471 files contain `DATA`.
- 576 files contain `POKE`.
- 563 files contain `FOR`, `NEXT`, and `TO`.
- 492 files contain `IF`; 484 contain `GOTO`; 470 contain `READ`.
- 188 files contain `PEEK`; 102 contain `SYS`; 6 contain `WAIT`.
- BASIC program size: min 12 bytes, median about 1.4 KiB, 90th percentile about
  7.2 KiB, max about 32 KiB.

Representative files:

- `DEMOS/0-9/128_Byte_Blues_BASIC.sid`: one short BASIC line plus machine code.
- `DEMOS/A-F/America_BASIC.sid`: large tokenized BASIC program, hundreds of
  lines.
- `DEMOS/A-F/Baroque_Music_64_BASIC.sid`: BASIC program with data tables and
  control flow.
- `MUSICIANS/B/Bond_Alan/Randomly_generated_music_BASIC.sid`: 256 failing
  subtunes, so one file can dominate all-subtune row counts.

Conclusion:

- BASIC belongs in scope for a good SID player, and the first implementation
  path is now in place, but this remains a distinct compatibility surface rather
  than a small PSID-style tweak.
- Most BASIC-RSID files are real tokenized BASIC music programs, not just
  `SYS` stubs into machine code.
- A meaningful fix needs a BASIC execution mode, not only a `SYS` pre-scan. The
  current interpreter covers common music-program statements, string variables
  and helpers, multidimensional array references, `DEF FN`, physical line-scan
  fallback for corrupted BASIC links, screen-memory `PRINT` side effects,
  non-returning `SYS` main loops, and a small clean-room set of BASIC-ROM parser
  stubs used by machine-code helper routines. Full ROM BASIC semantics remain
  open.

Required engine pieces:

- A tokenized Commodore BASIC V2 program reader for linked-line programs loaded
  at `$0801`.
- BASIC runtime state: variables, string variables, arrays, `DATA` pointer,
  `FOR/NEXT` stack, `GOSUB/RETURN` stack, line lookup, and current statement
  cursor.
- Expression evaluator with enough C64 BASIC behavior for addresses, arithmetic,
  comparisons, `RND`, `INT`, `ABS`, `PEEK`, `CHR$`, `ASC`, `LEN`, and string
  slicing where used by fixtures. The implemented clean-room path now also
  covers `STR$`, `VAL`, `LEFT$`, `RIGHT$`, `MID$`, `SGN`, `SQR`, `LOG`, `EXP`,
  `SIN`, `COS`, `TAN`, and `ATN`.
- Statement support at least for `POKE`, `FOR/NEXT`, `DATA/READ/RESTORE`,
  `RESTORE <line>`, `IF/THEN`, `GOTO`, `GOSUB/RETURN`, `END/STOP`, `PRINT`,
  `PRINT#`, `GET`, `INPUT`, `CLR`, `DIM`, `ON ... GOTO/GOSUB`, `SYS`, `WAIT`,
  and `REM`.
- A bridge from BASIC `POKE` to the same C64 bus used by the CPU and SID. POKEs
  to `$d400-$d41f` must affect SID state; POKEs to vectors and timers must
  update the emulated environment.
- A bridge from BASIC `SYS` to CPU execution. `SYS` should run machine code
  with the current bus/memory and defined register setup, not start a separate
  world.
- A timing model. Executing BASIC instantly will produce wrong tempo for many
  files. A first approximation can charge statement/token costs and advance the
  audio clock; later improvements can tune these costs against known BASIC SID
  playback.

Design recommendation:

- Keep the separate `basicRSID` engine path selected by RSID BASIC flag. Do not
  collapse it into the normal `init/play` path.
- Implement it clean-room rather than requiring bundled ROMs. Bundling original
  BASIC/KERNAL ROMs creates licensing and web-distribution problems.
- Keep an optional future ROM-backed mode possible, but do not make it the only
  compatibility path.
- Keep expanding the curated fixture set:
  - tiny `SYS` and `POKE` programs,
  - `DATA/READ/POKE/FOR` music loops,
  - `PEEK`/`WAIT` timing examples,
  - one large multi-subtune BASIC file.
  The current smoke covers these classes, including a passing non-returning
  `SYS` launcher.
- Current validation:
  - `go run ./cmd/hvsc-compat -duration 50ms -subtunes all -list /tmp/rasterklang-basic-files.txt ...`
    covers 587 BASIC files and 1,388 subtunes with 0 failures.
  - `go run ./cmd/hvsc-compat -duration 10s -subtunes default -min-rms 0.0005 -list /tmp/rasterklang-basic-files.txt ...`
    covers 587 default subtunes and leaves 1 silence row.
  - The remaining 10 second silence row is `Beat_Dis_BASIC`.
  - `Randomly_generated_music_BASIC`,
    `Randomly_generated_music_2_BASIC`, `SySound_BASIC`,
    `Music_Expansion_Demo_BASIC`, `Dance_into_the_Groove_BASIC`, and
    `Entertainer_BASIC` now pass the same 10 second silence threshold after
    adding clean-room FAC/number helpers, custom SYS/voice-command bridges, and
    memory-backed BASIC variable/array reads.
  - `Allt_Som_Jag_BASIC` now passes after initializing BASIC end pointers
    (`VARTAB`, `ARYTAB`, `STREND`) from the loaded payload end, matching the
    C64 LOAD environment expected by BASIC stubs with hidden appended data.
  - `Black_Box_V8_Demo_BASIC` now passes after recognizing its Sound
    Master/SAM installer and mapping the Sound Master music commands to SID
    register writes. Speech/SAM commands are still not synthesized.

Remaining BASIC groups:

- Custom BASIC speech system: `Beat_Dis_BASIC` installs or relies on a
  nonstandard interactive "C64 Speech System V2.7" command set. It has SID
  volume-DAC routines for `HEAR`, `RECORD`, and `PLAY`, but this SID file does
  not contain a deterministic scripted playback command to translate. It stays
  classified as `speech_extension`; auto-invoking an interactive `PLAY` path
  would be a fake pass.
- Speech BASIC extensions remain a separate concern. `Black_Box` contains
  `SAY`/control commands similar in shape to Magic Voice-style BASIC extensions
  (`SAY`, `RATE`, `VOC`, `RDY`). Magic Voice is the external-hardware case
  (https://www.c64-wiki.de/wiki/Magic_Voice); SAM/Reciter and C64 Speech System
  are separate software speech concerns. The current implementation only maps
  the tune's Sound Master music commands; it does not implement SAM, Reciter, or
  Magic Voice speech synthesis.

Drawbacks:

- This pulls rasterklang toward "small C64 runtime" territory.
- C64 BASIC numeric/string behavior is quirky. A Go-native approximation will
  pass many music programs, but not all BASIC edge cases.
- Tempo accuracy is a real risk unless the BASIC timing model is validated.
- Partial support must be reported clearly; otherwise "BASIC supported" will be
  interpreted as "full C64 BASIC compatibility".

## Track 2: RSID Startup/Main-Loop Execution

This track covers most of `init_cycle_limit` and part of `no_irq_vector`.

Snapshot evidence:

- `init_cycle_limit`: 1,431 rows, 1,058 files.
- 942 of those files have `play=$0000`.
- 444 have `init == load`, which often means the init address is a program entry
  point rather than a small player init routine.
- 537 have payloads larger than 32 KiB.
- Common init addresses: `$080D` (188 rows), `$1000` (130), `$C006` (130),
  `$1BDF` (64), `$A3A0` (44), `$4000` (43), `$C000` (41).
- `no_irq_vector`: 980 rows, 159 files, all with `play=$0000`.

Representative files:

- `DEMOS/0-9/12345.sid`: init at `$1500`, large RSID payload, init cycle limit.
- `DEMOS/A-F/Aztec_Beat.sid`: init at `$080D`, cycle limit.
- `DEMOS/A-F/Abyss_Zone_Demo.sid`: init returns but no IRQ vector is visible.
- `GAMES/A-F/Beach_Head_II.sid`: multiple subtunes fail with no IRQ vector.

Likely causes:

- Many RSID files are complete or partial C64 programs. They may not return
  from init in the PSID sense.
- Some code waits for raster, CIA timer, IRQ flags, keyboard/screen state, or
  KERNAL services.
- Current `$d012` and CIA behavior is static RAM, so wait loops can never make
  progress.
- Current startup success is too binary: return from synthetic subroutine or
  installed vector. Real programs can be "running correctly" without either.

Required engine pieces:

- A continuous C64 execution mode that advances CPU, audio, and hardware in
  cycle slices instead of only calling `init` as a bounded subroutine.
- Minimal VIC-II state:
  - raster line/cycle progression,
  - `$d011/$d012` reads,
  - raster IRQ compare,
  - `$d019` acknowledge behavior,
  - `$d01a` interrupt enable behavior.
- Minimal CIA Timer A/B state for `$dc04/$dc05/$dc0d/$dc0e` and optionally CIA2
  equivalents when needed.
- A startup runner with success criteria:
  - RTS from init,
  - non-default IRQ vector installed,
  - direct play address available,
  - stable main loop that writes SID or services interrupts,
  - timeout with diagnostic bucket if no progress.
- A main-loop stream mode for `play=0` tunes where the engine keeps executing
  the program and delivers interrupts instead of manually calling one IRQ
  routine per frame.

Design recommendation:

- Keep the existing direct `init/play` path as the fast path.
- Add an RSID/full-program path when `play=0`, when init hits a classified wait
  loop, or when the tune format/flags indicate strict C64 behavior.
- Make "progress" explicit: SID writes, IRQ vector changes, timer/raster state
  changes, audio samples emitted, and non-repeating PC windows.

Risks:

- This is architectural. It changes how time is represented in the engine.
- It can expose latent CPU/bus bugs because more code paths execute.
- If hardware modeling is too shallow, cycle-limit errors may just move to
  different addresses.

## Track 3: IRQ, BRK, KERNAL And ROM Handling

This track originally covered `BRK`, unsupported opcodes, and ROM/vector
behavior. BRK vectoring, 6510 KIL/JAM CPU halt behavior, memory
classification, and the first KERNAL IRQ-tail stubs are now implemented; the
remaining work is narrower and should be driven by observed ROM entry points
and long-handler traces.

Current evidence:

- BRK is no longer a top-level failure bucket.
- Unsupported opcode is no longer a failure bucket in the ordinary no-ESS
  full scan.
- ROM entry: 28 rows, 28 files, concentrated around remaining KERNAL/BASIC/I/O
  entries such as `$EA87`, `$FEBC`, `$FCE2`, and `$DC0C`.
- The direct IRQ model can return through `$EA31`, `$EA34`, `$EA7B`, `$EA7E`,
  and `$EA81`, but it is still not a full `$FFFE -> KERNAL prologue -> $0314`
  interrupt flow.

Representative files:

- `DEMOS/G-L/Kinetix_Blasting_Power_Mix.sid`: init BRK at `$2057`.
- `GAMES/M-R/Mystery_Voyage.sid`: IRQ BRK near low memory.
- `MUSICIANS/L/Lieblich_Russell/Little_Computer_People.sid`: still exposes
  long IRQ paths after its former KIL/JAM failures are treated as CPU halt.
- `DEMOS/A-F/Compotune_Digi.sid`: now classifies by startup/runtime behavior
  instead of a raw unsupported opcode near `$EA77`.

Likely causes:

- Real C64 `BRK` vectors through `$fffe/$ffff`; this is now implemented, but
  repeated BRK loops can still reveal wrong startup paths.
- Default IRQ/KERNAL paths are only partially implemented, so programs that jump
  deeper into KERNAL vectors or services can still fall into explicit
  `rom_entry` diagnostics.
- KIL/JAM bytes are not useful opcodes. They now model the 6510 CPU halt
  behavior and let the SID continue from its current register state.

Required engine pieces:

- CPU-level BRK handling that pushes PC/status and jumps through the vector,
  with guardrails for repeated BRK loops.
- A minimal KERNAL IRQ path compatible with common player endings such as
  `JMP $EA31`.
- Stubs for common KERNAL routines that RSID/BASIC startup can hit. These should
  be explicit and traced, not silent magic.
- Bus memory classification so diagnostics can say whether PC is in loaded
  payload, empty RAM, I/O, or emulated ROM/stub.
- Optional ROM abstraction:
  - clean-room KERNAL/BASIC stubs as default,
  - possible user-supplied ROM image later,
  - no bundled copyrighted ROM dependency.

Design recommendation:

- Keep KIL/JAM as CPU halt, not as a normal instruction family.
- Keep ROM/stub execution observable in debug output.

## Track 4: Long Play/IRQ Runtime Handlers

Current evidence:

- 32 rows, 26 files remain after KERNAL-hook and progress-overrun handling.
- 20 RSID rows and 12 PSID rows.
- The old `play_cycle_limit` targeted set dropped from 118 rows to 32 rows.
- Remaining rows are dominated by no-progress hardware/BRK/ROM-adjacent paths
  and very small CIA-frame budgets rather than the old `$C01F`/`$8048` active
  SID-writing cluster.
- Common symptoms:
  - `IRQ at $C01F exceeded 39408 cycles`
  - `subroutine at $09EF exceeded 32844 cycles`
  - `subroutine at $3B10 exceeded 49266 cycles`

Representative files:

- `DEMOS/A-F/Always_on_the_Run.sid`
- `DEMOS/A-F/Digidrum_Concert.sid`
- `GAMES/A-F/Alcatraz.sid`
- `GAMES/G-L/Kosmic_Kanga.sid`
- `GAMES/M-R/Phalsberg.sid`

Likely causes:

- Some handlers are valid but longer than `cyclesPerFrame * 2`, especially digi,
  speech, game, or sound-effect material. SID-writing overruns are now accepted
  as progress-making frames.
- Some handlers are actually waiting on hardware state that currently does not
  advance.
- Some direct-play PSID files may need a higher budget, but a blanket budget
  increase will hide real hangs.

Required engine pieces:

- Failure diagnostics that distinguish hardware wait loops, ROM/KERNAL paths,
  unchanged tight loops, and no-SID-write cycle limits.
- A scanner-only experimental frame-budget multiplier to quantify how many
  failures are merely over budget.
- Continuous scheduler fallback for handlers that do not return and need to
  resume from the current PC rather than being restarted each frame.

Design recommendation:

- Do not simply raise `maxPlayCycles` globally.
- For direct-play PSID, keep the bounded call as the default fast path. The
  current progress-overrun fallback should stay guarded by SID writes.
- For RSID `play=0`, prefer the full-program scheduler from Track 2.

## Recommended Implementation Sequence

1. Build the real full-RSID scheduler path.
   It should keep CPU, audio, VIC/CIA, IRQ delivery, and progress detection in
   one time model for `play=0` tunes that do not install a simple vector.

2. Expand KERNAL/BASIC/I/O stubs only from the current `rom_entry` evidence.
   `$EA87`, `$FEBC`, `$FCE2`, `$A000`, and `$DC0C` need per-address
   investigation before adding behavior.

3. Trace the remaining play/IRQ cycle limits before adding another fallback.
   Active SID-writing overrun is already handled; unchanged tight loops should
   still fail fast.

4. Keep BASIC on regression watch.
   The current 1 second all-subtune scan has zero BASIC failures; future work
   should preserve that while improving timing and speech-extension honesty.

## Future Subagent Work Packages

For the next implementation wave, split work by ownership to avoid conflicts:

1. External/speech policy worker.
   Owns policy for type-labeled speech-specialized clusters: whether to filter,
   document as unsupported external speech, or implement a real audio path.

2. Full-RSID scheduler worker.
   Owns `internal/engine` scheduling code. Turns the current continuous-init
   fallback into a real CPU/audio/VIC/CIA/IRQ main-loop mode.

3. KERNAL/ROM worker.
   Owns `internal/c64` plus tests. Investigates the remaining `rom_entry`
   addresses and adds explicit clean-room stubs only where the behavior is known.

4. Long-handler worker.
   Owns progress diagnostics and runtime budget behavior for direct play and IRQ
   routines.

5. Regression worker.
   Owns fixed fixture lists, full-corpus comparisons, and guardrails for the
   BASIC, KERNAL-tail, continuous-init, and long-handler representatives.

Each engine behavior change must update `docs/sid-engine-notes.md` with the
motivating tunes, whether the behavior is established C64/SID behavior or an
approximation, expected improvement, and remaining uncertainty.

## Validation Strategy

- Keep a fixed fixture list per bucket before changing behavior.
- For each track, measure:
  - total failure rows,
  - distinct failing files,
  - bucket distribution,
  - regressions in already working fixture tunes.
- Run after each meaningful change:

```sh
go test ./...
go run ./cmd/hvsc-compat -duration 1s -out /tmp/hvsc-compat-after.tsv <hvsc-root-or-fixture-list>
```

- Compare buckets, not just total failures. A good change may temporarily move
  rows from `init_cycle_limit` into a more precise bucket before eliminating
  them.
