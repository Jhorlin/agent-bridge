# Agent Bridge

Experimental, local-first synchronization between Claude Code and Codex configuration.

**Status: experimental Go synchronizer with shared instruction sections, portable skills, bounded MCP/plugin translation, minimal agents, startup-hook configuration, explicit profile inheritance, and pinned native symlinks. This is not full Claude/Codex feature parity.** No global installation, live configuration changes, or background service registration happens during setup.

## Run

Start with [convention-based setup](docs/conventions.md): select a project or an
explicit global root once, then discover instructions, skills, agents, hooks, MCP
and plugin authoring packages, including new components while watching. Source
builds only; not included in the published alpha. Plugin installation, credentials
and security enforcement remain host-managed. Existing instruction-only profiles
stay instruction-only until explicitly upgraded. The [onboarding guide](docs/onboarding.md)
also covers manual resource exceptions.

Requires Go 1.25+ to build. The resulting standalone executable does not require Go installed to run. TOML and YAML parsing use pinned pure-Go dependencies. macOS and Linux are supported. Windows filesystem safety/permissions are not implemented yet.

`./agent-bridge --version` identifies the build (`dev` for ordinary source builds).
The [distribution guide](docs/distribution.md) covers Go-only cross-platform
packaging, archive verification and installation. Download the experimental
[v0.1.0-alpha.1 prerelease](https://github.com/Jhorlin/agent-bridge/releases/tag/v0.1.0-alpha.1)
for standalone macOS/Linux binaries and SHA-256 checksums. Start with temporary
fixtures; this alpha is not recommended for security-sensitive production settings.

```sh
go test -race ./...
go vet ./...
go build -o agent-bridge ./cmd/agent-bridge
./agent-bridge plan examples/bridge.json
./agent-bridge audit examples/bridge.json
./agent-bridge audit examples/mcp.bridge.json --json
# Inspect resolved profiles and paths (conventions inspect native metadata):
./agent-bridge config examples/bridge.json
./agent-bridge sync examples/bridge.json
./agent-bridge watch examples/bridge.json
# Explicit opt-in to writes during polling:
./agent-bridge watch examples/bridge.json --apply
# Recover an interrupted transaction after inspecting its journal:
./agent-bridge recover examples/bridge.json
```

The example synchronizes only demo files and the ignored `.agent-bridge` directory. Current source builds also write private, rotating diagnostics for mutating commands in the OS user's log/state directory, outside synchronized files; preview commands do not create those logs. Watch mode polls every second; without `--apply` it only reports changes. Applying requires two identical consecutive observations of the resolved profile and raw inputs, followed by a recheck under the write lock. This adds at least one polling interval before a change is applied. Stop with Ctrl-C.

On macOS, opt-in `service install|start|stop|status|uninstall` commands manage a per-user background watcher. Installation is preview-only unless you explicitly pass `--apply`; uninstall preserves native files, state, backups and logs. See the [service setup and lifecycle guide](docs/services.md) before enabling it.

Exit codes: 0 = successful command (a read-only plan may report pending work), 1 = usage or operational error, 2 = synchronization conflict. Watch mode reports conflicts and keeps checking until stopped. Unreadable or unsupported inputs pause writes and are retried, with redacted pause/resume diagnostics; transaction and output errors stop the watcher. It never automatically recovers a pending transaction. Signals finish the current sync before shutdown.

If a file changes between a reviewed observation and the pre-journal input check,
watch mode discards that stale plan and retries. No journal or native writes are
made for that attempt; the new contents must stabilize before synchronization.

`audit CONFIG [--json]` is a read-only compatibility preflight for explicit and convention-discovered resources. It reports adapter/direction, recognized native MCP/plugin manifest fields, unsupported fields, redacted unknown-field counts, and host-local follow-up actions. Exit 2 means at least one resource is blocked (including conflicts, invalid content or unsafe state); exit 0 still requires human compatibility review, not host certification. Invalid profiles exit 1. No locks, state, backups, native files, environment expansion, installation or authentication are performed. See [audit details](docs/adapters.md#compatibility-audit).

Before global onboarding, run `inventory-skills ABSOLUTE_HOME --include-plugin-cache`
for a [read-only duplicate and native-variant preflight](docs/skill-inventory.md).
It distinguishes shared links, identical/different copies, system collisions and
cached plugin candidates without copying or enrolling them. Cache presence is not
proof of enablement; the report is advisory and does not change existing watchers.

Source builds also provide [whole-resource retirement that preserves files](docs/retirement.md),
[reviewed supporting-file rename/delete and historical undo](docs/supporting-file-changes.md)
with retained backups and interrupted-operation recovery. These explicit commands
do not make ordinary synchronization propagate deletions automatically.

`compare-plugin-copy CONFIG ID SIDE ABS_COPY` [checks an explicitly selected
plugin copy for stale authoring files](docs/plugin-copy.md), without invoking
installers or changing native enablement, authentication or trust.

## Logs and troubleshooting

Current source builds provide private rotating JSON logs and sanitized diagnostics:

```sh
./agent-bridge doctor /absolute/path/bridge.json
./agent-bridge logs /absolute/path/bridge.json --tail 100
./agent-bridge support-bundle /absolute/path/bridge.json /absolute/path/new-support.json
```

`doctor` is read-only and reports service mode/status, locks, pending recovery and
recent events. `logs` locates logs and maps hashed references back to local files;
keep that mapping private. `support-bundle` creates a new private file excluding
configuration, credentials, raw service output and recovery snapshots. Review it
before sharing; nothing is uploaded. Structured event retention is 4 MiB per
profile. These commands are not in the published alpha; see the
[troubleshooting, privacy and error-code guide](docs/diagnostics.md).

## Architecture

![Agent Bridge Go architecture: explicit profiles feed the CLI; adapters normalize three local peers for baseline reconciliation; opt-in transactions journal and apply guarded writes.](docs/architecture.svg)

[Interactive diagram](docs/architecture.html) · [PNG](docs/architecture.png) · [Diagram source and validation notes](docs/architecture-notes.md). Download the HTML and open it locally to explore components and code references; GitHub displays the HTML source rather than running the viewer.

Each managed file (explicitly registered or convention-discovered) has three peers: a shared-store file, a Claude path, and a Codex path. A manifest records their last synchronized content/executable-bit SHA-256 digest. Changes to any one peer propagate to the others. Different concurrent edits to the same file block the entire sync. Identical concurrent edits converge. This is baseline-based reconciliation, not last-writer-wins copying.

Configuration paths resolve relative to their declaring file; absolute paths are supported. Profiles can explicitly `extends` a global/base profile and replace whole resources by ID or `disable` inherited resources. `scope` remains a label: Agent Bridge does not emulate either host's instruction-loading precedence or automatically discover projects. Start with sandbox fixtures, not your home configuration. See [configuration and adapter reference](docs/adapters.md).

The `portable-file` adapter copies exact bytes. Use it only when the content is genuinely compatible with both tools. It does not claim arbitrary CLAUDE.md instructions, skill metadata, or scripts are behaviorally portable.

When a project directory has `.claude/CLAUDE.md`, current source builds use a
[reversible instruction set](docs/instruction-sets.md): both Claude instruction
files stay separate, while Codex receives source-marked sections in `AGENTS.md`.
Edits inside those sections synchronize back to the corresponding source.

## Skill directories

Register **one skill directory per resource**, not the entire installed-skills folder. All nested regular files (including hidden and binary files) participate; review the directory for secrets before adoption. Existing populated peers must contain `SKILL.md`. By default, the adapter preserves bytes and executable bits without validating frontmatter or tool behavior. Set `portable: true` only after reviewing that compatibility yourself.

```json
{
  "version": 1,
  "stateDir": ".agent-bridge",
  "resources": [{
    "id": "demo-skill",
    "kind": "skill-directory",
    "portable": true,
    "scope": "project",
    "claude": "sandbox/claude/skills/demo",
    "codex": "sandbox/codex/skills/demo"
  }]
}
```

New files from either peer are adopted automatically within the explicitly registered directory. Independent changes to different files merge. Deleted tracked files block synchronization; renames therefore require manual reconciliation. Empty directories are not mirrored. Existing root-level native symlinks require explicit `linkTargets` pins; nested links and hardlinks remain rejected.

For opt-in **strict common metadata**, add `"allowReformat": true` to a new skill resource. This validates and semantically reconciles `name`, `description`, and bounded informational `license`/`compatibility`/`metadata`, preserves instruction bytes and supporting files, and rejects unknown/host-specific metadata and `agents/openai.yaml`. Formatting-only frontmatter edits no longer cause drift. This is not invocation-policy or script translation. See [strict skill mode](docs/skill-metadata.md), including safe adoption of existing resources.

Source builds additionally offer `translateSkillInvocation: true` with strict mode:
Claude's `disable-model-invocation` maps to the inverse Codex
`policy.allow_implicit_invocation` in a transactionally paired sidecar. Explicit
invocation remains available; host-specific skill controls remain unsupported. See
[invocation policy, adoption and native test limits](docs/skill-invocation.md).

With invocation translation, opt-in `preserveSkillSettings: true` retains bounded
Claude-only grants, hints and custom dependency/version metadata in its original
file while syncing portable instructions. It never grants Codex extra approvals.
The same option is available under `conventions`; see
[native-local skill settings](docs/skill-settings.md).

For reviewed Claude path-only edit guards, the opt-in
[`hook-file-guard` runtime adapter](docs/file-guards.md) translates Codex patch
targets into legacy path-bearing hook inputs. Source builds also offer an explicit
`file-guard-config` adapter for ongoing bidirectional reference changes within a
reviewed script allowlist, preserving other native hooks. Neither is selected
automatically or claims full hook or permission equivalence.

For minimal Claude plugin agents, source builds support explicit or convention-derived,
namespaced [standalone Codex agent exports](docs/plugin-agent-exports.md).
Those files have an independent lifecycle; Codex plugin uninstall does not remove them.

Conventional plugin commands also retain bounded Claude-local `model`,
`allowed-tools` and `argument-hint` settings while synchronizing static text.
Explicit profiles opt in with `preserveCommandSettings: true` and
`allowReformat: true`; plugin conventions enable it automatically. Those settings
are not exported as Codex permissions or model choices. See
[command mapping and limits](docs/plugin-commands.md).

## New adapters

See the [compatibility matrix and implementation priorities](docs/compatibility.md) for current coverage, test evidence, and features that remain host-specific or intentionally excluded. File synchronization is not full behavioral compatibility.

| Capability | Supported now | Explicit limits |
| --- | --- | --- |
| Shared instructions | Automatic root/nested project pairs and a selected global pair; optional explicit shared sections | No semantic translation of instructions, imports or precedence |
| Custom agents | Name, description and instruction body; Claude Markdown/YAML ↔ Codex TOML; source builds offer bounded host-local settings retention | Settings are not equivalent cross-host permissions; unsupported fields rejected |
| Hooks | Explicitly timed startup SessionStart definitions; source builds also map UserPromptSubmit, Stop and exact-Bash PreToolUse/PostToolUse, plus opt-in reviewed path-guard references | No trust grants or script execution during sync; the path-guard runtime separately executes reviewed policies, with bounded inputs and stronger failure handling, not general hook parity |
| MCP | Explicit allowlist or convention-discovered names; stdio/HTTP; Claude JSON ↔ Codex TOML; environment/header/bearer references; bidirectional ongoing sync | Literal env/header credentials, unsupported policy fields, SSE, interpolation in command/args/URL, partial explicit allowlists, and deleting tracked servers block sync |
| Plugins | Portable skill packages, common metadata, supporting files; source builds add bounded conventional hooks, static commands, selected MCP and explicit or convention-derived standalone agent exports | No automatic installation/cache refresh, OAuth, marketplace management, portable-layout hooks/MCP/commands, direct bundled Codex agents, app mappings or arbitrary host-specific fields |
| Symlinks | Existing native file/skill/plugin root link pinned to an explicit existing physical target; link preserved on writes | No link creation, nested/chained links, target changes, or overlapping targets |
| Inheritance | Explicit base/global profile, declaring-file-relative paths, full-resource project overrides, disabling inherited resources | No automatic project discovery, host instruction inheritance or partial-field merging; cross-profile coordination/enrollment is separately explicit |

MCP and plugin entries are compared semantically; formatting-only differences do not cause sync loops. Compiled outputs are parsed back and checked against the canonical model before writing. All adapters use the same guarded transaction journal and conflict blocking. MCP translation preserves unrelated setting **values**. Source builds also preserve surrounding layout/comments for scalar edits (including same-length arrays and inline tables) when a verified text patch is possible; structural changes still reformat and can remove TOML comments. Each MCP resource requires `allowReformat: true`; see [format preservation limits](docs/mcp-formatting.md).

Source builds can also retain bounded Codex-local MCP policies with an explicit
opt-in; these are **not** translated into Claude permissions. See [policy retention](docs/adapters.md#retaining-codex-local-mcp-policies-source-builds).

Try `./agent-bridge plan examples/mcp.bridge.json`, then `sync` with the same file. It generates an isolated example TOML config under `examples/sandbox`; it does not launch a server, authenticate, or contact the example endpoint.

See [shared instructions, agents and hooks](docs/portable-adapters.md) for consent requirements, supported fields and sandbox examples.
Source builds can retain bounded host-local agent settings with
`preserveAgentSettings`; this does not translate models or permissions between hosts.

## Recovery

Before changing any target, a private journal records all before/after snapshots, including the manifest. A pending marker blocks new syncs until the transaction commits or is recovered. Ordinary write failures trigger rollback automatically. If a later edit differs from both recorded snapshots, rollback stops and preserves that edit.

New syncs create absent targets exclusively and record successful creations and replacements in a separate private receipt. Recovery refuses to delete or revert a file without the corresponding recorded write—even if another editor produced identical bytes. Missing/invalid required receipts or interruption between a write and its receipt require inspection; pending state remains intact. See [recovery compatibility](docs/upgrading.md#creation-ownership-recovery).

`recover CONFIG` rolls back the pending transaction; it does not restore arbitrary historic backups. Inspect `stateDir/pending.json` and its referenced `backups/<transaction>/journal.json` privately. If a process was killed, inspect the PID in `sync.lock`, confirm that no writer remains, and remove only that stale lock before running recovery. The CLI never steals a lock automatically. Retain the same configuration paths during recovery. Backups remain after recovery; newly created empty directories may remain too.

Agent Bridge uses config schema 1, manifest schema 2, and recovery-journal schema 1. Resource options are recorded in resource identity; changing an already adopted binding requires a new ID or separately reviewed adoption. Unsupported state schemas are rejected. No live state migration runs automatically.

## Safety and limits

- `plan` never writes. `sync` explicitly applies changes. Summaries omit file contents.
- Conflicting initial copies require manual reconciliation before adoption.
- Deletions are conflicts; no automatic pruning.
- Only explicitly pinned native root symlinks are accepted. Symlinked parents, nested/chained links, and hard-linked files are rejected. Pins are rechecked before writes.
- Writes use fsynced sibling temporary files, with rename for replacements and exclusive hard-link creation for absent targets. Private before/after snapshots and creation receipts are retained under `stateDir/backups`.
- A state-directory lock excludes writers using the same state. Profiles can share an explicit `coordinationDir` to serialize sync and recovery across different state directories; see [coordination setup](docs/onboarding.md#multiple-profiles). Profiles using different or omitted coordination directories are not coordinated. External editors are not locked; a remaining check/write race exists. Do not use for security-sensitive production configuration yet.
- Multi-file changes are recoverable but **not atomically visible**. External readers may observe partial progress. Process-interruption recovery is tested; full power-loss durability and adversarial filesystem races are not guaranteed.
- New files use private read/write permissions plus source executable bits. Existing target read/write permissions are preserved. ACLs, ownership, extended attributes, timestamps, and directory metadata are not mirrored.
- State/backups can contain sensitive content. Keep them local, outside public Git, and do not configure credentials as portable files.
- OAuth/session tokens, permission policy, arbitrary hook behavior, and semantic instruction translation are not implemented. Startup-hook configuration and minimal agent definitions are supported only within the documented subset. Unsupported fields/components fail explicitly. Plugin packages are authored, not installed or enabled.
- Resource paths cannot overlap (conservative case-insensitive comparison on every OS). Changing the paths/kind/scope of an already managed ID requires new explicit adoption. Config files and state directories must be trusted and kept private.

## Release boundary and future work

Source builds provide `systemd-unit ABSOLUTE_PROFILE ABSOLUTE_BINARY [--apply]`
to export a Linux user-unit proposal to stdout. It does not install or start a
service; preview mode is the default. See [Linux unit export](docs/phase-two.md#linux-unit-export)
for limitations. Source builds also include an experimental owned Linux lifecycle;
[native user-manager lifecycle acceptance passed in isolated Linux CI](docs/services.md#linux-systemd-user-services-experimental-source-builds).

The next phase is tracked in the [eight-workstream acceptance plan](docs/phase-two.md).
Its [public upstream fixture catalog](internal/bridge/testdata/upstream/README.md)
records pinned sources and separate third-party licenses; fixtures are inert test
data, not installed plugins. Phase-two features are not part of the published alpha.
Source builds also provide `watch-discovery`, `draft-profile` and `check-overlap`; see the plan
for their read-only behavior, usage and limits. None enrolls resources or
enforces persistent cross-profile ownership.
For cooperating profiles, source builds can enforce an explicitly reviewed
coordinator roster during sync/recovery; see [opt-in ownership enforcement](docs/phase-two.md#opt-in-ownership-enforcement).
`review-profile` and the explicit write command `sync-reviewed` add a freshness
check between review and application; see [guarding a reviewed sync](docs/phase-two.md#guarding-a-reviewed-sync).
`enroll-reviewed` registers an existing reviewed profile in its coordinator roster
without syncing native files; see [reviewed enrollment](docs/phase-two.md#enrolling-an-existing-reviewed-profile).
For journaled creation and registration together, use [review-enrollment and create-enrolled](docs/enrollment-creation.md).
`review-resolution` and `resolve-reviewed` support explicit, freshness-checked
conflict choices; see [resolving conflicts](docs/conflict-resolution.md).
`history`, `review-history` and `restore-reviewed` select retained portable file
versions without restoring an old manifest; see [historical selection](docs/conflict-resolution.md#selecting-a-retained-historical-version).

The agreed bounded feature set is implemented: shared instruction sections,
portable skills (with opt-in strict common metadata), selected MCP configuration,
skill-only plugin packages, minimal agents, startup-hook configuration,
audit/recovery, and global/project onboarding. macOS background service management
is also implemented. See the [release readiness and validation record](docs/release-readiness.md)
for exact scope, evidence and exclusions. This remains experimental, not full
Claude/Codex parity or a claim of 100% testing.

Remaining expansion beyond the bounded current source implementation:

- Stronger filesystem-race/power-loss guarantees, broader metadata preservation
  and automatic/native-entry-point deletion policies. Reviewed supporting-file
  changes and bounded historical restore already exist.
- Enrollment without per-candidate review, host instruction-precedence emulation,
  and nested-link support. Read-only discovery and opt-in ownership coordination
  already exist.
- Richer skill argument/dependency semantics and broader host-policy equivalence;
  see the existing bounded [invocation translation](docs/skill-invocation.md).
- Structural MCP formatting preservation and package-relative paths beyond the
  existing [per-server merging](docs/phase-two.md#per-server-mcp-merging) and
  [scalar text patches](docs/mcp-formatting.md).
- In-package Codex agents, package-root relocation and production native plugin
  install/cache refresh. Selected plugin MCP/commands/hooks and explicit standalone
  agent exports already exist; see [phase-two scope](docs/phase-two.md).
- More hook tools/events, richer agents, broader real-model execution evidence,
  reboot/login coverage and automatic binary upgrades. Experimental macOS/Linux
  service lifecycle and structured diagnostics are implemented.

## Prior art

Design references, not vendored implementations:

- [agent-config-sync](https://github.com/B0llerwagen/agent-config-sync): normalized adapters and reviewable plans.
- [gaal](https://github.com/getgaal/gaal): centralized configuration, scopes, reconciliation.
- [agent-sync-template](https://github.com/benthamite/agent-sync-template): counterpart audits and behavioral compatibility.

This independent implementation is available under the [MIT License](LICENSE).
