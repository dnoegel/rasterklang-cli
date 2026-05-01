# Labeling Feasibility

Generated: 2026-04-30T09:34:38+02:00

Sample: 24 music-only SID records, 30s render per tune at 22050 Hz.

## Verdict

- Strong tune labeling looks feasible from V4 symbolic tokens for structure-first labels such as form, phrase role, motif return, contour, and filter motion.
- Cheap audio features help most as weak confirmation for energy, repetition, and rough section contrast, not as primary truth.
- The clearest V5-token candidates from this run are `BASS_PATTERN` and `DRUM_THEME`; `ENERGY_CURVE` should stay derived.

## Scope

- Source corpus slice: `MUSICIANS` tunes from the V4 token dataset
- Primary evidence: symbolic V4 form/motif/phrase tokens
- Secondary evidence: cheap audio features from rendered WAV output
- External model: not used in this run

## Coverage

- Unique forms in sample: 5
- Tunes with drums: 24
- Tunes with filter motion: 24

## Example Artifacts

- Pattern example: `gpt-eval/examples/pattern_analysis.v1.json`
- Audio semantics example: `gpt-eval/examples/audio_semantics.v1.json`
- Semantic analysis example: `gpt-eval/examples/semantic_analysis.v1.json`

## Label Table

| label | best source | confidence | failure modes | should become V5 token? |
| --- | --- | --- | --- | --- |
| FORM | symbolic | high | coarse phrase grid | already in V4 |
| PHRASE_ROLE | symbolic | high | bridge/fill boundaries are heuristic | already in V4 |
| MOTIF_RETURN | symbolic | high | variation families collapse when phrase labels drift | derive or make explicit |
| MOTIF_CONTOUR | symbolic | medium-high | lead-lane assignment can miss polyphonic handoff | already in V4 |
| MOTIF_RHYTHM | symbolic | medium-high | coarse grid hides swing and ornament clusters | already in V4 |
| BASS_PATTERN | symbolic | medium | voice 2 is not always true bass | yes |
| DRUM_THEME | symbolic | medium | noise hits and pitched events blur together | yes |
| FILTER_MOTION | symbolic | high | register motion does not equal audible salience | maybe |
| ENERGY_CURVE | hybrid | medium | token density and RMS diverge on airy passages | no |
| SECTION_ROLE | symbolic | medium | intro/main/break labels are broad | maybe |

## Notes

- The run is music-only and samples from the `MUSICIANS` V4 dataset slice.
- Audio features are weak evidence only; no external audio model is used in this experiment.
