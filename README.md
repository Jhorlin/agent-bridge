# Agent Bridge

Experimental, local-first synchronization between Claude Code and Codex configuration.

**Status: working portable-file prototype, not a complete configuration translator.** No global installation, live configuration changes, or background service registration happens during setup.

## Run

Requires Node.js 22+. No third-party runtime dependencies.

```sh
npm test
node bin/agent-bridge.js plan examples/bridge.json
node bin/agent-bridge.js sync examples/bridge.json
node bin/agent-bridge.js watch examples/bridge.json
# Explicit opt-in to writes during polling:
node bin/agent-bridge.js watch examples/bridge.json --apply
```

The example touches only demo files and the ignored `.agent-bridge` directory. Watch mode polls every second; without `--apply` it only reports changes. Stop with Ctrl-C.

## Architecture

Each explicitly registered portable file has three peers: a shared-store file, a Claude path, and a Codex path. A manifest records their last synchronized SHA-256 digest. Changes to any one peer propagate to the others. Different concurrent edits block the entire sync. Identical concurrent edits converge. This is baseline-based reconciliation, not last-writer-wins copying.

Configuration paths resolve relative to the configuration file; absolute paths are supported. `scope` labels global/project resources but does **not** implement inheritance or automatically discover projects. Start with sandbox fixtures, not your home configuration.

The `portable-file` adapter copies exact bytes. Use it only when the content is genuinely compatible with both tools. It does not claim arbitrary CLAUDE.md instructions, skill metadata, or scripts are behaviorally portable.

## Safety and limits

- `plan` never writes. `sync` explicitly applies changes. Summaries omit file contents.
- Conflicting initial copies require manual reconciliation before adoption.
- Deletions are conflicts; no automatic pruning.
- Symlinks are rejected, including symlinked parent paths.
- Writes use sibling temporary files and rename. Existing contents are backed up with a journal under `stateDir/backups`.
- A lock excludes other bridge writers. External editors are not locked: a remaining check/write race exists. Do not use for security-sensitive production configuration yet.
- Multi-file writes are **not transactional**. A failure may leave partial updates; backups support manual recovery. Automated restore and crash recovery are pending.
- Files are written with private permissions (0600). Executable mode preservation is not implemented.
- State/backups can contain sensitive content. Keep them local, outside public Git, and do not configure credentials as portable files.
- No MCP, OAuth/session-token, plugin, hook, permission, recursive skill-directory, or semantic instruction translation exists yet. Unsupported adapter kinds fail explicitly.

## Roadmap / acceptance gates

1. Harden reconciliation: fault injection, races, journal recovery/restore, metadata preservation, ownership and deletion policy.
2. Explicit global/project precedence and project registration; preserve unrelated native configuration.
3. Skill-directory adapter with auxiliary files and tool-specific compatibility reports.
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
