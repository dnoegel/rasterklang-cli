# Third-Party Notices

Rasterklang is licensed under the MIT License. See `LICENSE`.

This repository depends on third-party Go modules declared in `go.mod` and
locked in `go.sum`. Release archives are built from the exact tagged source
state, so this notice must be reviewed whenever `go.mod` or `go.sum` changes.

Known runtime dependency surface for the CLI release:

| Component | Version | License | Use |
| --- | --- | --- | --- |
| `github.com/ebitengine/oto/v3` | v3.4.0 | Apache-2.0 | Native audio output. |
| `github.com/ebitengine/purego` | v0.9.0 | Apache-2.0 | Indirect native library calls used by Oto on supported platforms. |
| `golang.org/x/sys` | v0.36.0 | BSD-3-Clause | Platform syscall support used by the audio/runtime stack. |

The CLI release archives are built from source and do not intentionally include
the HVSC tune corpus, C64 ROM images, or third-party SID files.

Before each public release, generate and review a complete dependency license
report from the exact tagged source state.
