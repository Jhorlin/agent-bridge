# Architecture diagram

Current source also expands a selected project through convention-based
instruction discovery before adapter planning. Derived file pairs use the same
baseline, journal, conflict and recovery paths shown below; no separate copying
daemon is introduced. See [convention-based instructions](conventions.md).

The [README image](architecture.svg), [PNG](architecture.png), and [interactive HTML](architecture.html) describe the core Go synchronization architecture at the verified revision below, not every subsequent feature. The HTML is standalone: download it and open it in a browser. It supports themes, zoom, component inspection, code references, and clean image exports. The CLI remains a standalone Go executable; the documentation viewer is not a runtime dependency.

Current source adds a content-free observer path from the CLI and transaction
engine to `internal/diagnostics`. Its private rotating files live outside the
shared/native/state peers shown in the diagram. Read-only diagnostic commands
inspect selected-profile state and re-encode validated events into sanitized
bundles; they do not feed log data into synchronization. See the
[diagnostic architecture and privacy boundary](diagnostics.md) for that addition.

The [Archify specification](architecture.json) is the editable source. Its code references were verified against commit `b64fa028bb39e53892a4cdecc54a5b156ec0be3b`. Arrows summarize component responsibilities, not every function call: recovery uses the same transaction module, and adapter rendering and round-trip verification occur before writes. The three peers are local paths, not remote services. “Private local state” means files on disk, not a database server.

## Validation receipt

- Diagram type: architecture.
- Archify showcase: 9/9 checks passed; 0 errors, 0 warnings.
- Repository evidence: 11 verified source references.
- Automated browser evidence: passed at 1440×900, 1600×1000, 1920×1080, and 2048×1320, with no page overflow.
- Visual review: passed after inspection of light and dark screenshots; labels and routes are separated and the composition fits the desktop viewer.
- Correction rounds: 1.
- Specification SHA-256: `abd998bc3c1406d8b153e5cf1a9a0a70400eff74583c099fe2e8beba6abb79ac` (3,289 bytes).
- HTML SHA-256: `f7824d1bd658c8feef1d41badc856d495ca7f131c50251c8d9f469cbc5389f2a` (712,431 bytes).

SVG and PNG were generated through the accepted HTML's built-in export menu. They are presentation assets, not substitutes for the HTML validation receipt.
