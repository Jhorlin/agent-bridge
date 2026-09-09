# Bounded release readiness

Status on 2026-09-08: the agreed initial implementation boundary is covered by
code and tests and is available as public Go source. It remains **experimental**.
The first tagged prerelease is [v0.1.0-alpha.1](https://github.com/Jhorlin/agent-bridge/releases/tag/v0.1.0-alpha.1),
with standalone macOS/Linux assets. This record is not a claim of universal host
interoperability or 100% testing.

This page records the original alpha boundary, not the expanded current source.
Current source also includes [rotating diagnostics and sanitized support bundles](diagnostics.md),
with their own privacy/fault-injection tests and validation record. This is not a
new tagged release or a change to the immutable alpha assets.
See [phase-two progress](phase-two.md) and [upgrade boundaries](upgrading.md) for
subsequent work. A native-plus-upgrade-plus-race run at source commit `e1a0b17` measured **84.4%
overall statement coverage** (bridge 85.4%, CLI 82.9%, release helpers 69.1%).
That run passed using disposable homes and fake local providers; optional native
service/package tests were not enabled in that coverage invocation. The upgrade
test built the pinned alpha source and current source; it did not replace an
installed binary or publish a release.

[Distribution packaging](distribution.md) is implemented separately: four
platform archives, checksums, version reporting and CI smoke tests. Each subsequent
publication requires reviewed artifacts, verified CI and an explicit release step.

## Implemented boundary

- Shared instruction sections, preserving host-only surrounding text.
- Portable skill directories; optional strict name/description reconciliation
  with exact instruction-body bytes and rejection of unsupported metadata.
- Named MCP allowlists for bounded stdio/HTTP configuration translation, without
  exporting literal credentials or granting native approvals.
- Portable skill-only plugin authoring directories and supported manifest layouts;
  installation and cache refresh remain explicit native-host actions.
- Minimal agent name, description and instructions in both host formats.
- Explicitly timed startup-only command-hook configuration, without copying trust
  or promising equivalent arbitrary script behavior.
- Read-only audit/discovery, empty-profile creation, explicit global/project
  inheritance, optional common coordination, guarded synchronization and recovery.
- Ongoing polling with stable-input checks, conflict/deletion blocking and retry
  after invalid inputs; macOS opt-in background service management and safe removal.
- Go-only application build/runtime; MIT license and user-facing setup/reference docs.

Unsupported fields/components are not silently translated. Raw byte-copy modes
still require explicit human portability review; they are not metadata validators.

## Validation record

For implementation commit `30840f3`, local validation passed:

```sh
AGENT_BRIDGE_NATIVE_TESTS=1 AGENT_BRIDGE_SERVICE_TESTS=1 \
  go test -race -coverprofile=/tmp/agent-bridge-skill-coverage.out ./...
go vet ./...
go build ./cmd/agent-bridge
go test ./internal/bridge -run '^$' -fuzz '^FuzzStrictSkillRoundTrip$' -fuzztime=10s -parallel=2
```

Statement coverage in this run was **86.2% overall**: bridge 87.0%, CLI 81.1%.
Executable child-process paths are tested but not instrumented into the parent
coverage profile (`main` therefore reports 0%). Coverage measures executed Go
statements, not every input, filesystem failure or native-host behavior.

The native fixtures used Claude Code 2.1.266 and Codex CLI 0.153.4 on macOS 26.4.1.
Tests used temporary homes/profiles, a tiny local Go MCP server and loopback fake
model providers. No real credentials were copied, paid requests made or production
configuration changed. Missing host binaries or GUI access skip native checks and
must never be counted as successful acceptance on another machine.

GitHub CI runs race tests, vet and builds on macOS/Linux; Linux additionally fuzzes
MCP, agent, hook and strict-skill adapters. Cross-builds cover macOS/Linux on amd64
and arm64. CI does not replace the separately recorded native-host checks.
See [workflow runs](https://github.com/Jhorlin/agent-bridge/actions/workflows/test.yml)
for each commit's actual result; this document does not imply future runs passed.

## Known exclusions, not silent completion claims

No OAuth/token synchronization, policy/permission weakening, arbitrary hook events,
rich agent orchestration, universal skill semantics, bundled MCP/plugin components,
automatic native installation/cache refresh, automatic resource enrollment, Linux
service installer, or host instruction-precedence emulation is included. MCP
formatting may change and its selected server set reconciles as one unit.

External edit/check-write races and partial multi-file visibility remain. Neither
full power-loss durability nor hostile concurrent filesystem mutation is certified.
Real-model behavior, every host version, login/reboot and arbitrary script failure
semantics are unverified. Do not use this experimental version for security-sensitive
production configuration.

These boundaries distinguish completed implementation from optional expansion.
Before expanding a claim, add forward/reverse, rejection/conflict, privacy,
rollback and relevant native tests, then update the [compatibility matrix](compatibility.md).
