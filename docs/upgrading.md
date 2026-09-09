# Upgrade validation and safe boundaries

Source builds have an opt-in cross-version test against the actual published
`v0.1.0-alpha.1` source at commit
`e22b5b23f11f8f1f9c26ed511416800678ca14fd`:

```sh
AGENT_BRIDGE_UPGRADE_TESTS=1 go test -race ./internal/bridge -run '^TestPublishedAlphaUpgrade$' -count=1 -v
```

The repository must contain that Git object (CI checks out full history). The
test exports only that pinned tree into a disposable directory, rejects unsafe
archive entries, builds it and today's source with Go, and uses temporary homes
and profiles. It neither downloads/runs a release asset nor changes installed
binaries, services, native application configuration or credentials.

Seven legacy resource fixtures cover raw instructions, marked instruction
sections, strict common skills, MCP, minimal agents, startup hooks and skill-only
plugins. Tests sync with the old binary, upgrade without rewriting native data,
propagate edits with the new writer, and run reverse edits with the old writer.
The unchanged legacy feature subset can then sync with the new writer again.
Another fixture recreates a pending marker for a real alpha-written journal and
verifies current recovery followed by successful sync. This is a simulated
interruption, not a power-loss test. It does not certify every historical release,
operating system, configuration, permission, or downgrade after using new features.

## Before changing an installed binary

Stop all affected watchers/services. Keep a private backup of the profile, state,
coordinator and native data, plus the old binary; those backups may contain
sensitive material. Resolve pending recovery with a compatible binary before
upgrading. Review the new capability boundaries and use `audit`, `plan` and a
fresh `review-profile`/`sync-reviewed` before restarting in apply mode.

Do not mix old and new writers against new options or pending-operation formats.
New resources/consent flags are not implicitly enabled by upgrading. Downgrades
after adopting new features are **not supported** by this evidence. Older code
does not know newer ownership/pending markers, and may not safely interpret new
configuration choices. Never delete baselines or pending markers to force it.

Linux CI separately tests a real disposable user manager's lifecycle/restart and
same-source, version-labeled binary replacement. That service test and this
cross-version data/journal test are distinct: neither implies arbitrary-version
service migration, automatic upgrade installation or successful reboot/login.
No published alpha asset is modified by these tests.

## Diagnostic logging in source builds

Current source binaries automatically log mutating command attempts and applying
watchers to a private per-profile OS log/state directory. These are separate from
sync state and the legacy service stdout/stderr streams. No profile migration or
consent flags are needed; read-only commands still create no structured logs.
`doctor`, `logs` and `support-bundle` are new commands, unavailable in the immutable
published alpha. See [locations, retention, privacy and troubleshooting](diagnostics.md).
The diagnostic schema does not change config, manifest or recovery-journal schemas.

## Creation-ownership recovery

Config schema 1, manifest schema 2, and the recovery journal's version-1 encoding
are unchanged. New syncs write a **version-2 ownership receipt** in
`creation-ownership.json` beside `journal.json`, and set `creationOwnership: true`
and `writeOwnership: true` in their pending pointer. This receipt is bound to the
journal and records successful creations and replacements for every operation,
including manifest writes. Recovery validates it before changing any file.
Matching after-bytes alone are not ownership evidence: an unrecorded changed file
blocks recovery and retains all evidence, even if another editor saved the exact
version the bridge intended to write.

If a process stops between any native write and recording its receipt,
ownership is ambiguous even when the bytes match. Inspect and preserve that file;
do not delete the receipt or pending marker to bypass the check. If another writer
created it, moving that file aside to a safe location after inspection lets
recovery roll back the bridge's other writes without deleting the external file.
For an existing-file replacement, preserve the external edit separately and
restore the recorded before snapshot only after inspection before retrying
recovery. The bridge never performs that manual reconciliation automatically.

Version-1 creation receipts remain readable for creations. They cannot prove
replacement ownership, so a changed existing target without replacement evidence
now blocks recovery conservatively. New pending pointers requiring write evidence
reject downgraded version-1 receipts; missing version-2 write flags also fail closed.

Legacy pending pointers without the flag or a receipt remain readable using legacy recovery
semantics, as verified by the pinned-alpha test. They cannot provide creation
ownership evidence retroactively. Never use an older binary to recover a new
pending transaction: older code either rejects the new format or ignores flags
and lacks this protection.
These private local records are not signed ownership proofs against tampering,
and full power-loss/adversarial filesystem-race guarantees remain out of scope.
