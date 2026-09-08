# Go migration

## Layout

- `cmd/agent-bridge`: binary entry point and signal handling.
- `internal/cli`: argument validation, JSON output, exit codes, cancellable polling.
- `internal/bridge/config.go`: config loading and scope/path validation.
- `internal/bridge/plan.go`: per-file three-peer reconciliation and summaries.
- `internal/bridge/transaction.go`: lock, journal, guarded writes and recovery.
- `internal/bridge/filesystem*.go`: snapshots, permissions, no-follow opens and atomic replacement.

Only the standard library is used. CI tests macOS and Linux with the race detector and vet, and cross-builds Darwin/Linux amd64/arm64 executables. Build products are not committed. Windows remains unsupported until its filesystem semantics have dedicated implementation and tests.

## Compatibility contract

Config version 1, manifest version 2, journal version 1, resource IDs, shared-store paths, and native paths remain unchanged. Fingerprints are SHA-256 of compact JSON `[base64Contents, executableBits]`, exactly as in Node v0.2. Missing snapshots remain JSON null. Snapshot mode values remain decimal JSON integers containing Unix permission bits. Resource path keys retain shared/claude/codex order for the old runtime's JSON-string-based identity check.

An unchanged existing manifest is not rewritten just to change formatting. Changing an existing resource's ID binding, kind, or scope is still rejected. Legacy v0.1 state remains rejected. Rollback validates every journal target and snapshot before any restore and refuses to discard later edits.

## Original 30 scenarios

The engine tests cover: read-only planning; idempotent bootstrap; edits from each peer; initial divergence; conflicting edits; identical edits; deletion protection; symlink rejection; private journals; writer locks; unsupported adapters; nested skill bytes/executables; independent skill edits/additions; skill-wide conflict blocking; support-file deletion; malformed/symlinked skills; portability opt-in; mode-only changes; partial-write rollback; bootstrap cleanup; later-edit recovery refusal; process interruption/stale locks; nested path rejection; resource rebinding; no-op transaction avoidance; hardlinks; reserved IDs; and recovery target validation.

The remaining two original scenarios are in CLI tests: watch/apply/shutdown and conflict exit code 2 without writes. Additional Go tests cover usage errors, read-only watch, recover/plan commands, malformed recovery snapshots, legacy manifests, and Node encoding compatibility. A subprocess helper intentionally exits mid-transaction to exercise real interruption; it is skipped when run outside that subprocess.

## Known limits retained

Polling is still one second, not native filesystem notifications. Global/project scope is explicit, not inheritance. No semantic skill translation, symlink support, MCP/plugin translation, token copying, service installation, or new pruning behavior was introduced by the language switch. Atomic replacement is per file, not a multi-file transaction visible to readers. Parent-path/check-write races and full power-loss durability remain open work.
