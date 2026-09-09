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
