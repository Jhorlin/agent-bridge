# Agent Bridge

Experimental, local-first synchronization between Claude Code and Codex configuration.

**Status: v0.4 experimental Go synchronizer with portable files, skill packages, bounded MCP/plugin translation, explicit profile inheritance, and pinned native symlinks. This is not full Claude/Codex feature parity.** No global installation, live configuration changes, or background service registration happens during setup.

## Run

Requires Go 1.25+ to build. The resulting standalone executable does not require Go installed to run. TOML parsing uses the pinned pure-Go `github.com/pelletier/go-toml/v2` dependency. macOS and Linux are supported. Windows filesystem safety/permissions are not implemented yet.

```sh
go test -race ./...
go vet ./...
go build -o agent-bridge ./cmd/agent-bridge
./agent-bridge plan examples/bridge.json
./agent-bridge audit examples/bridge.json
./agent-bridge audit examples/mcp.bridge.json --json
# Inspect resolved profiles and target paths without reading native contents:
./agent-bridge config examples/bridge.json
./agent-bridge sync examples/bridge.json
./agent-bridge watch examples/bridge.json
# Explicit opt-in to writes during polling:
./agent-bridge watch examples/bridge.json --apply
# Recover an interrupted transaction after inspecting its journal:
./agent-bridge recover examples/bridge.json
```

The example touches only demo files and the ignored `.agent-bridge` directory. Watch mode polls every second; without `--apply` it only reports changes. Stop with Ctrl-C.

Exit codes: 0 = successful command (a read-only plan may report pending work), 1 = usage or operational error, 2 = synchronization conflict. Watch mode reports conflicts and keeps checking until stopped; operational errors stop it. Signals finish the current sync before shutdown.

`audit CONFIG [--json]` is a read-only compatibility preflight for explicitly registered resources. It reports adapter/direction, recognized native MCP/plugin manifest fields, unsupported fields, redacted unknown-field counts, and host-local follow-up actions. Exit 2 means at least one resource is blocked (including conflicts, invalid content or unsafe state); exit 0 still requires human compatibility review, not host certification. Invalid profiles exit 1. No locks, state, backups, native files, environment expansion, installation or authentication are performed. See [audit details](docs/adapters.md#compatibility-audit).

## Architecture

![Agent Bridge Go architecture: explicit profiles feed the CLI; adapters normalize three local peers for baseline reconciliation; opt-in transactions journal and apply guarded writes.](docs/architecture.svg)

[Interactive diagram](docs/architecture.html) · [PNG](docs/architecture.png) · [Diagram source and validation notes](docs/architecture-notes.md). Download the HTML and open it locally to explore components and code references; GitHub displays the HTML source rather than running the viewer.

Each explicitly registered file has three peers: a shared-store file, a Claude path, and a Codex path. A manifest records their last synchronized content/executable-bit SHA-256 digest. Changes to any one peer propagate to the others. Different concurrent edits to the same file block the entire sync. Identical concurrent edits converge. This is baseline-based reconciliation, not last-writer-wins copying.

Configuration paths resolve relative to their declaring file; absolute paths are supported. Profiles can explicitly `extends` a global/base profile and replace whole resources by ID or `disable` inherited resources. `scope` remains a label: Agent Bridge does not emulate either host's instruction-loading precedence or automatically discover projects. Start with sandbox fixtures, not your home configuration. See [configuration and adapter reference](docs/adapters.md).

The `portable-file` adapter copies exact bytes. Use it only when the content is genuinely compatible with both tools. It does not claim arbitrary CLAUDE.md instructions, skill metadata, or scripts are behaviorally portable.

## Skill directories

Register **one skill directory per resource**, not the entire installed-skills folder. All nested regular files (including hidden and binary files) participate; review the directory for secrets before adoption. Existing populated peers must contain `SKILL.md`. The adapter preserves bytes and executable bits; it does not translate frontmatter or validate tool behavior. Set `portable: true` only after reviewing that compatibility yourself.

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

## New adapters

See the [compatibility matrix and implementation priorities](docs/compatibility.md) for current coverage, test evidence, and features that remain host-specific or intentionally excluded. File synchronization is not full behavioral compatibility.

| Capability | Supported now | Explicit limits |
| --- | --- | --- |
| MCP | Named allowlist; stdio/HTTP; Claude JSON ↔ Codex TOML; environment/header/bearer references; bidirectional ongoing sync | Literal env/header credentials, unsupported policy fields, SSE, interpolation in command/args/URL, partial allowlists, and deleting selected servers block sync |
| Plugins | Portable skill-package directories, common manifest metadata, supporting files; Claude compatibility manifest ↔ Codex compatibility or portable manifest | No installation/cache refresh, OAuth, marketplace management, hooks, agents, bundled MCP, app mappings, custom component paths, or host-specific fields |
| Symlinks | Existing native file/skill/plugin root link pinned to an explicit existing physical target; link preserved on writes | No link creation, nested/chained links, target changes, or overlapping targets |
| Inheritance | Explicit base/global profile, declaring-file-relative paths, full-resource project overrides, disabling inherited resources | No automatic project discovery, host instruction inheritance, partial-field merging, or multi-profile coordination |

MCP and plugin entries are compared semantically; formatting-only differences do not cause sync loops. Compiled outputs are parsed back and checked against the canonical model before writing. All adapters use the same guarded transaction journal and conflict blocking. MCP translation preserves unrelated setting **values**, but rewrites formatting and can remove TOML comments; each MCP resource requires `allowReformat: true`.

Try `./agent-bridge plan examples/mcp.bridge.json`, then `sync` with the same file. It generates an isolated example TOML config under `examples/sandbox`; it does not launch a server, authenticate, or contact the example endpoint.

## Recovery

Before changing any target, a private journal records all before/after snapshots, including the manifest. A pending marker blocks new syncs until the transaction commits or is recovered. Ordinary write failures trigger rollback automatically. If a later edit differs from both recorded snapshots, rollback stops and preserves that edit.

`recover CONFIG` rolls back the pending transaction; it does not restore arbitrary historic backups. Inspect `stateDir/pending.json` and its referenced `backups/<transaction>/journal.json` privately. If a process was killed, inspect the PID in `sync.lock`, confirm that no writer remains, and remove only that stale lock before running recovery. The CLI never steals a lock automatically. Retain the same configuration paths during recovery. Backups remain after recovery; newly created empty directories may remain too.

Agent Bridge uses config schema 1, manifest schema 2, and recovery-journal schema 1. Resource options are recorded in resource identity; changing an already adopted binding requires a new ID or separately reviewed adoption. Unsupported state schemas are rejected. No live state migration runs automatically.

## Safety and limits

- `plan` never writes. `sync` explicitly applies changes. Summaries omit file contents.
- Conflicting initial copies require manual reconciliation before adoption.
- Deletions are conflicts; no automatic pruning.
- Only explicitly pinned native root symlinks are accepted. Symlinked parents, nested/chained links, and hard-linked files are rejected. Pins are rechecked before writes.
- Writes use fsynced sibling temporary files and rename. Private before/after snapshots are retained in journals under `stateDir/backups`.
- A state-directory lock excludes writers using the same state. Do not run profiles concurrently if they target overlapping native paths: different state directories are not coordinated. External editors are not locked; a remaining check/write race exists. Do not use for security-sensitive production configuration yet.
- Multi-file changes are recoverable but **not atomically visible**. External readers may observe partial progress. Process-interruption recovery is tested; full power-loss durability and adversarial filesystem races are not guaranteed.
- New files use private read/write permissions plus source executable bits. Existing target read/write permissions are preserved. ACLs, ownership, extended attributes, timestamps, and directory metadata are not mirrored.
- State/backups can contain sensitive content. Keep them local, outside public Git, and do not configure credentials as portable files.
- OAuth/session tokens, hook behavior, permission policy, and semantic instruction translation are not implemented. Unsupported fields/components fail explicitly. Plugin packages are authored, not installed or enabled.
- Resource paths cannot overlap (conservative case-insensitive comparison on every OS). Changing the paths/kind/scope of an already managed ID requires new explicit adoption. Config files and state directories must be trusted and kept private.

## Roadmap / acceptance gates

The [native host acceptance harness](docs/native-testing.md) checks generated MCP configuration and portable package validation with disposable host configuration directories. These checks do not certify model behavior or full host interoperability.

1. Further hardening: filesystem races, power-loss durability, richer metadata preservation, ownership and deletion policy, safe historical restore.
2. Project discovery and multi-profile coordination; host-level instruction inheritance.
3. Semantic skill compatibility reports and safe nested-link support (root pins and recursive file syncing are implemented).
4. Broader MCP coverage, per-server reconciliation, and comment-preserving editing (bounded JSON/TOML adapters are implemented).
5. Structured instructions with shared content and tool-specific overlays.
6. Richer plugin components, bundled MCP, hooks, and installed-cache lifecycle; no blanket parity claims.
7. Watcher lifecycle, debouncing, onboarding previews, safe uninstall and macOS service integration.

## Prior art

Design references, not vendored implementations:

- [agent-config-sync](https://github.com/B0llerwagen/agent-config-sync): normalized adapters and reviewable plans.
- [gaal](https://github.com/getgaal/gaal): centralized configuration, scopes, reconciliation.
- [agent-sync-template](https://github.com/benthamite/agent-sync-template): counterpart audits and behavioral compatibility.

This repository currently contains an independent implementation. Public visibility does not itself grant an open-source license; license selection remains pending.
