# Agent Bridge

Experimental, local-first synchronization between Claude Code and Codex configuration.

**Status: v0.3 Go implementation of the experimental portable-file and skill-directory synchronizer, not a complete configuration translator.** No global installation, live configuration changes, or background service registration happens during setup.

## Run

Requires Go 1.25+ to build. The resulting executable needs neither Go nor Node installed to run. Standard library only; no third-party dependencies. macOS and Linux are supported. Windows filesystem safety/permissions are not implemented yet.

```sh
go test -race ./...
go vet ./...
go build -o agent-bridge ./cmd/agent-bridge
./agent-bridge plan examples/bridge.json
./agent-bridge sync examples/bridge.json
./agent-bridge watch examples/bridge.json
# Explicit opt-in to writes during polling:
./agent-bridge watch examples/bridge.json --apply
# Recover an interrupted transaction after inspecting its journal:
./agent-bridge recover examples/bridge.json
```

The example touches only demo files and the ignored `.agent-bridge` directory. Watch mode polls every second; without `--apply` it only reports changes. Stop with Ctrl-C.

Exit codes: 0 = successful command (a read-only plan may report pending work), 1 = usage or operational error, 2 = synchronization conflict. Watch mode reports conflicts and keeps checking until stopped; operational errors stop it. Signals finish the current sync before shutdown.

## Go migration

The CLI command names and config schema 1 are unchanged. Replace `node bin/agent-bridge.js` with `./agent-bridge`. There is no npm setup step.

Existing Node v0.2 manifest schema 2 and journal schema 1 remain supported, including the exact SHA-256 fingerprint algorithm and resource property order. Cross-runtime verification covered Node-state adoption, unchanged-manifest preservation, edits in both runtimes, and recovery of an interrupted Node transaction using Go. Stop any Node watcher before switching: do not run both writers concurrently. A stale lock still requires inspection, not automatic removal.

The Node sources and tests were replaced by Go; they remain recoverable in Git at commit `9141381`. The original 30 behavior scenarios are represented in the Go engine/CLI tests, with additional migration and validation cases. See [migration notes](docs/go-migration.md).

## Architecture

Each explicitly registered file has three peers: a shared-store file, a Claude path, and a Codex path. A manifest records their last synchronized content/executable-bit SHA-256 digest. Changes to any one peer propagate to the others. Different concurrent edits to the same file block the entire sync. Identical concurrent edits converge. This is baseline-based reconciliation, not last-writer-wins copying.

Configuration paths resolve relative to the configuration file; absolute paths are supported. `scope` labels global/project resources but does **not** implement inheritance or automatically discover projects. Start with sandbox fixtures, not your home configuration.

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

New files from either peer are adopted automatically within the explicitly registered directory. Independent changes to different files merge. Deleted tracked files block synchronization; renames therefore require manual reconciliation. Empty directories are not mirrored. Existing symlink-based skill installations are not supported yet.

## Recovery

Before changing any target, a private journal records all before/after snapshots, including the manifest. A pending marker blocks new syncs until the transaction commits or is recovered. Ordinary write failures trigger rollback automatically. If a later edit differs from both recorded snapshots, rollback stops and preserves that edit.

`recover CONFIG` rolls back the pending transaction; it does not restore arbitrary historic backups. Inspect `stateDir/pending.json` and its referenced `backups/<transaction>/journal.json` privately. If a process was killed, inspect the PID in `sync.lock`, confirm that no writer remains, and remove only that stale lock before running recovery. The CLI never steals a lock automatically. Retain the same configuration paths during recovery. Backups remain after recovery; newly created empty directories may remain too.

Versions 0.2 and 0.3 use manifest schema 2. Version 0.1 manifests are rejected rather than silently reinterpreted. Preserve old state/backups and use a fresh state directory for explicit re-adoption; divergent files require review. No live state migration runs automatically.

## Safety and limits

- `plan` never writes. `sync` explicitly applies changes. Summaries omit file contents.
- Conflicting initial copies require manual reconciliation before adoption.
- Deletions are conflicts; no automatic pruning.
- Symlinks are rejected, including symlinked parent paths; hard-linked files are also rejected.
- Writes use fsynced sibling temporary files and rename. Private before/after snapshots are retained in journals under `stateDir/backups`.
- A lock excludes other bridge writers. External editors are not locked: a remaining check/write race exists. Do not use for security-sensitive production configuration yet.
- Multi-file changes are recoverable but **not atomically visible**. External readers may observe partial progress. Process-interruption recovery is tested; full power-loss durability and adversarial filesystem races are not guaranteed.
- New files use private read/write permissions plus source executable bits. Existing target read/write permissions are preserved. ACLs, ownership, extended attributes, timestamps, and directory metadata are not mirrored.
- State/backups can contain sensitive content. Keep them local, outside public Git, and do not configure credentials as portable files.
- No MCP, OAuth/session-token, plugin, hook, permission, or semantic instruction translation exists yet. Unsupported adapter kinds fail explicitly.
- Resource paths cannot overlap (conservative case-insensitive comparison on every OS). Changing the paths/kind/scope of an already managed ID requires new explicit adoption. Config files and state directories must be trusted and kept private.

## Roadmap / acceptance gates

1. Further hardening: filesystem races, power-loss durability, richer metadata preservation, ownership and deletion policy, safe historical restore.
2. Explicit global/project precedence and project registration; preserve unrelated native configuration.
3. Skill compatibility reports, discovery, and safe handling of existing symlink installations (recursive file syncing is implemented).
4. MCP JSON/TOML adapters; preserve environment references without copying tokens or relaxing permissions.
5. Structured instructions with shared content and tool-specific overlays.
6. Plugin/hook capability inventory and explicit unsupported/lossy mappings.
7. Watcher lifecycle, debouncing, onboarding previews, safe uninstall and macOS service integration.

## Prior art

Design references, not vendored implementations:

- [agent-config-sync](https://github.com/B0llerwagen/agent-config-sync): normalized adapters and reviewable plans.
- [gaal](https://github.com/getgaal/gaal): centralized configuration, scopes, reconciliation.
- [agent-sync-template](https://github.com/benthamite/agent-sync-template): counterpart audits and behavioral compatibility.

This repository currently contains an independent implementation. Public visibility does not itself grant an open-source license; license selection remains pending.
