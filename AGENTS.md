# Agent Instructions

## SID Engine Documentation

When changing SID playback, timing, emulation behavior, audio quality, or sound
profile constants, update `docs/sid-engine-notes.md` in the same change.

That document must stay current about:

- what changed
- why it changed
- which tune or audible problem motivated it
- whether the change is well-established SID behavior or a tuned approximation
- what improvement is expected
- what uncertainty remains

Do not leave SID-engine heuristics undocumented. If a change introduces or
retunes a guessed parameter, explicitly mark it as approximate and explain how it
should eventually be validated.

