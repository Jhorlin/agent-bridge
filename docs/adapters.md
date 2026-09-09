# Configuration and adapter reference

All examples are sandbox-relative. Never start by pointing an unreviewed profile at your real home configuration. `plan` reads and reports; `sync` writes; `watch --apply` repeats guarded syncs. `config` shows effective resource paths without reading their contents.

## Global/base and project profiles

Each file has config `version: 1`, its own `stateDir`, and a `resources` array (which can be empty). `extends` names one parent profile. The child overrides whole resources by matching ID. Fields are not merged: an override must supply its complete definition. Paths resolve relative to the file that declared them, not the child working directory. Cycles and depth over 32 are rejected.

```json
{
  "version": 1,
  "stateDir": ".agent-bridge",
  "extends": "../base/bridge.json",
  "disable": ["optional-base-skill"],
  "resources": [{
    "id": "instructions",
    "kind": "portable-file",
    "scope": "project",
    "claude": "sandbox/project/CLAUDE.md",
    "codex": "sandbox/project/AGENTS.md"
  }]
}
```

The base must define the disabled ID. Disabling removes it from this profile only; no files are deleted. Unchanged inherited resources retain their original native paths, including global paths. To make a project-specific version, override the resource with project-local destinations. A child state directory contains its own shared store and baselines. This is profile composition, not automatic replication of every global item into every repository. Run one profile per set of native targets; profiles with overlapping targets must not run concurrently.

## Existing symlinks

Standalone `skill-directory` resources optionally accept `allowReformat: true`
for [strict common metadata](skill-metadata.md). Without it, files remain raw
byte copies. Switching an existing resource's mode requires reviewed adoption
under a new identity or fresh state; it is not an in-place baseline migration.

Add `linkTargets` to a resource to pin a native root link to a specific physical target:

```json
{
  "id": "shared-skill",
  "kind": "skill-directory",
  "portable": true,
  "scope": "global",
  "claude": "sandbox/claude/skills/demo",
  "codex": "sandbox/codex/skills/demo",
  "linkTargets": {"claude": "sandbox/shared/demo"}
}
```

The Claude path must already be a symlink resolving directly to the specified target. The target must exist and must not itself contain symlinked parents. Writes update the pinned physical files, never replace the link. A changed, missing, chained, or cyclic link is rejected. Pins are rechecked before every write. Links are not created automatically, and two resources/peers cannot alias overlapping physical destinations. Nested skill-file links are still rejected. Recovery uses the original physical targets; keep the original pin configuration available.

## MCP configuration

```json
{
  "id": "selected-tools",
  "kind": "mcp-config",
  "scope": "project",
  "claude": "sandbox/.mcp.json",
  "codex": "sandbox/.codex/config.toml",
  "servers": ["docs"],
  "allowReformat": true
}
```

Claude inputs use a top-level `mcpServers` object; Codex inputs use `mcp_servers` TOML tables. This also supports explicitly configured user-level files with those top-level structures. Nested local-project entries inside Claude's user config are not extracted. Only allowlisted servers are synchronized. Group all servers sharing a config-file pair into one resource; overlapping config paths are rejected.

The entire selected set is one reconciliation unit. Independent edits to different selected servers on opposite peers conflict conservatively. Each peer must have all selected servers or none, preventing a partial initial target from being silently overwritten. Deleting selected entries is blocked. Unselected servers and unrelated settings retain their values. JSON formatting changes; TOML formatting and comments can be lost, hence explicit reformat consent. No-op syncs do not rewrite comments.

| Common setting | Claude representation | Codex representation |
| --- | --- | --- |
| Stdio command/arguments | `command`, `args` | `command`, `args` |
| Working directory | absolute `cwd` | absolute `cwd` |
| Forward environment variable | `env: {"TOKEN":"${TOKEN}"}` | `env_vars = ["TOKEN"]` |
| HTTP endpoint | `type: "http"`, `url` | `url` |
| Bearer reference | `headers: {"Authorization":"Bearer ${TOKEN}"}` | `bearer_token_env_var = "TOKEN"` |
| Other header reference | `headers: {"X-Tenant":"${TENANT}"}` | `env_http_headers = {"X-Tenant":"TENANT"}` |

Environment values are never expanded by Agent Bridge. OAuth/session credentials are not read or transported. Literal env/header values, environment renaming, remote env sources, fallback expressions, command/argument/URL interpolation, URL user-info/query/fragment, SSE, and unknown selected-server options are rejected. Codex tool restrictions, enabled flags, timeouts, special authentication modes, and helper commands are not discarded or translated. An entry containing them blocks conversion.

This is not a general secret scanner: a credential embedded in an arbitrary command argument or file may still be copied. Review inputs. Journals hold private before/after snapshots of the whole native file, which can include unrelated sensitive values; keep state/backups private and out of Git. Logs and parse errors omit configuration contents. The bridge does not start MCP servers or verify network/authentication behavior.

Schema references used for these mappings: [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) and [Claude MCP](https://code.claude.com/docs/en/mcp). The implementation intentionally supports a narrower subset than either host.

## Plugin packages

```json
{
  "id": "portable-bundle",
  "kind": "plugin-directory",
  "portable": true,
  "scope": "project",
  "claude": "sandbox/claude-plugin",
  "codex": "sandbox/codex-plugin",
  "codexPluginLayout": "portable"
}
```

Claude uses `.claude-plugin/plugin.json`. By default Codex uses `.codex-plugin/plugin.json`; set `codexPluginLayout: "portable"` for root `plugin.json` with the Agent Plugins 1.0.0 schema. The shared store uses `plugin.json`. Do not switch layout on an adopted resource without reviewing a new adoption.

Common identity/publisher metadata is synchronized semantically. Skills in `skills/<name>/SKILL.md`, supporting `scripts/`, `assets/`, `references/`, root README and LICENSE files are synchronized per file, including executable bits. Compatibility manifests declare the standard skills path; portable Codex manifests rely on standard directory discovery. Custom skill locations are rejected. `portable: true` acknowledges that the skill content was reviewed; it is not an automated behavioral-equivalence certification.

Unsupported files or fields block the entire conversion: bundled MCP, hooks, agents, commands, app mappings, settings, host-specific presentation/options, and unrecognized root schemas. No installation, marketplace edits, cache refresh, enablement, trust changes, or login happens. A changed source package may therefore require an explicit host reinstall/refresh before its installed copy changes. This feature keeps registered authoring directories in sync, not installed marketplace caches.

Packaging references: [OpenAI package formats](https://developers.openai.com/plugins/build/plugins) and [Claude plugin reference](https://code.claude.com/docs/en/plugins-reference).

## Compatibility audit

Run `agent-bridge audit CONFIG` for text or `agent-bridge audit CONFIG --json` for JSON report schema version 1. This command uses the existing planner independently for each registered resource so one failure does not hide the rest. It reads native files, shared contents, and existing state without writing. It does not enumerate unregistered resources or run either host. Invalid profiles, including unknown options in inherited profiles, are rejected before auditing resources.

Resources are sorted by ID; native field checks are ordered Claude then Codex. The direction describes the adapter's bidirectional capability, not a selected winner for the next sync. `recognizedFields` lists present known top-level MCP server or plugin manifest keys, not a guarantee their values are valid; validation can still block the resource. `unsupportedFields` names only known unsupported keys. Arbitrary unknown keys are counted and redacted. MCP checks cover only selected servers and aggregate field names across that set. Nested metadata/component failures and shared-store problems can produce a resource-level blocker without a field-level explanation.

Every nonblocked resource is `review-required`, never “fully compatible.” `hostVerified` is false. A missing native side is reported as absent; an existing empty MCP document can have no selected fields. Host-local actions explain remaining review, authentication, or installation needs. `plan` remains the command for detailed synchronization decisions. Audit itself does not compile proposed outputs or certify subsequent apply; inputs may change after either read-only command.

Audit reports expose profile resource IDs, kinds and scopes, but not paths, native values, server names, unknown key names, or raw parser errors. Do not put secrets in resource IDs. Literal secrets inside arbitrary files/arguments are not detected; this is not a secret scanner. Profile errors are deliberately generic and go to stderr even with `--json`. Exit 0 means no detected blockers, exit 2 means at least one blocked resource, and exit 1 means invalid usage/profile or output failure. These audit-specific exit semantics do not change other commands.

## Verification coverage

Go tests cover bidirectional MCP and plugin conversion, mixed-resource all-or-nothing conflicts, per-file skill changes, exact rollback, unsafe credential/reference rejection, duplicate JSON keys, malformed TOML, integer precision, inherited path resolution/overrides, symlink pins and retargeting, and CLI watch/config behavior. Compiled adapter outputs are normalized again before writes. Fuzz tests exercise MCP JSON parsing. These are isolated filesystem/configuration tests, not live host, OAuth, or marketplace integration certification.
