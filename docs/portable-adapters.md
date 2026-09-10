# Shared instructions, agents, and startup hooks

These adapters use the same conflict detection, journal, rollback and polling engine as MCP. They do not execute instructions, agents or hooks. All require `portable: true` after reviewing content. Agent and hook adapters also require `allowReformat: true`. Explicit profiles select individual files, not entire settings directories. [All-feature conventions](conventions.md) derive those selections and strict adapter options automatically; review the inventory and plan before syncing. New adapter kinds require the current binary; do not downgrade an adopted profile to an older binary.

## Shared instruction sections

Use `kind: "instruction-file"` with Claude `CLAUDE.md` and Codex `AGENTS.md` paths. Each existing native file must contain exactly one ordered marker pair:

```markdown
Host-specific instructions can stay here.

<!-- agent-bridge:shared:start -->
Run tests before handing off code changes.
<!-- agent-bridge:shared:end -->

More host-specific instructions can stay here.
```

Only bytes between the markers are shared. Prefix and suffix remain exactly as they were on each host. The shared store contains the common body without markers. Editing the common block on either host propagates; different concurrent common-block edits conflict. Host-only edits do not cause common-block conflicts and remain local. Missing native files get a marker-wrapped body; existing unmarked files are rejected, not overwritten. Markers themselves cannot be nested or duplicated. Removing a file still conflicts.

This is explicit content ownership, not translation of imports, scoped rules, tool names or instruction precedence. A host-only edit can still change how the model interprets the common text. Review both files.

## Minimal custom agents

Agent files and decoded metadata must be valid UTF-8. Invalid byte sequences,
including invalid text produced by YAML binary scalars, are rejected rather than
silently replaced during translation. A fuzz-generated case is retained in the
regression corpus.

Use `kind: "agent-file"` with a Claude agent `.md` path and a Codex agent `.toml` path. The portable fields are name, description and instruction body. YAML parsing uses pinned pure-Go `gopkg.in/yaml.v3`; there is no additional application runtime.

Claude source:

```markdown
---
name: reviewer
description: Review code for correctness.
---
Inspect the proposed changes and explain concrete risks.
```

Codex representation:

```toml
name = 'reviewer'
description = 'Review code for correctness.'
developer_instructions = 'Inspect the proposed changes and explain concrete risks.'
```

Instruction body whitespace is preserved after normalizing Claude CRLF input to LF. Frontmatter/TOML formatting may change. Unknown fields, duplicate fields, missing metadata and empty instructions fail. Models, tools, permissions, sandbox settings, skill preloads, memory and agent lifecycle are not mapped. Do not remove safety settings to force an existing agent through this adapter: keep that agent host-local instead. Generated minimal agents inherit host defaults; this is not a security-policy equivalence claim. Local fake-provider tests verify Claude loads the translated agent instructions and Codex advertises the translated agent description. Real-model behavior and subagent orchestration remain unverified.

Native schemas: [Claude agents](https://code.claude.com/docs/en/sub-agents), [Codex agents](https://learn.chatgpt.com/docs/agent-configuration/subagents).

### Retaining host-local agent settings (source builds)

Set `"preserveAgentSettings": true` on an `agent-file` resource to retain a bounded
set of native settings while synchronizing name, description and instructions.
The same opt-in is available for explicitly configured
[plugin agent exports](plugin-agent-exports.md), with their separate lifecycle.
The default strict mode above is unchanged. This option is part of resource
identity and requires reviewed adoption, not an in-place toggle on a tracked ID.

Claude retains `model`, `tools`, `disallowedTools`, `permissionMode` and positive
integer `maxTurns`, and a nonempty single-line `color` of at most 64 bytes.
Color stays Claude-local; it is not a Codex field or execution policy.
Codex retains `model`, `model_reasoning_effort`, `sandbox_mode`
and string `approval_policy`. Types and bounded mode values are checked. Model
availability and the validity of particular tool names still require native review.
Unrecognized settings, hooks, MCP definitions, skill preloads and memory remain
blocked. YAML aliases, anchors and custom tags are rejected in this mode.

These settings remain in their original host file; they are not copied into the
shared model or translated to the other host. A newly generated counterpart has
native defaults until configured independently. **A read-only agent on one side
does not imply a read-only agent on the other.** Do not run either until its local
settings are appropriate. Audit highlights this distinction.

Policy-only edits stay local, but invalidate a prior raw-input review checkpoint.
Conflict choices and historical portable-content restores retain today's native
settings. Regenerated metadata may lose comments/formatting, not the supported
setting values. Tests cover bidirectional updates, rollback, invalid metadata,
consent/identity, stale review and historical restoration. Disposable native
fixtures exercise loading after updates with retained settings; no live-provider
calls or cross-host permission-equivalence claims are involved.

## Startup hooks

Use `kind: "hook-config"` for Claude `settings.json` and Codex `hooks.json`. This adapter maps the entire top-level `hooks` object as one unit. Other top-level values remain local and are preserved semantically; JSON formatting is rewritten. The first portable hook subset is intentionally narrow:

```json
{
  "hooks": {
    "SessionStart": [{
      "matcher": "^startup$",
      "hooks": [{
        "type": "command",
        "command": "/absolute/shared/startup-hook",
        "timeout": 10
      }]
    }]
  }
}
```

Source builds accept synchronous `SessionStart` (exact `^startup$` matcher), `UserPromptSubmit` and `Stop` command handlers, plus [exact-Bash pre/post tool hooks](tool-hooks.md). Prompt/Stop groups must omit `matcher` rather than relying on a host to ignore it. Explicit integer timeouts support 1–600 seconds. Omitted native timeouts are materialized from the source: Claude prompt hooks default to 30 seconds, Codex to 600; other supported events default to 600 on both. Shared canonical data requires explicit timeouts. The published alpha remains startup-only. Standalone commands must be clean absolute paths made of letters, digits, underscore, dot, dash and slash. Compatibility plugins additionally support bounded [package-relative sh/bash scripts and file arguments](plugin-runtime-paths.md). Other arguments, shell expressions, variable expansion, alternate shells, async, prompt/agent/MCP handlers, extra fields and other events are rejected. Standalone executables are not copied or checked by this adapter; register reviewed supporting files separately and verify their availability on both hosts.

The command must be manually reviewed for host-independent behavior. Both hosts expose startup context, but payload details, environment, error handling, output limits and model interpretation are not guaranteed equivalent. Use a harmless context-producing script first. No permission-decision or tool-event hook mapping is claimed. Trust and enablement stay host-local; the bridge never grants trust. Writing a Claude settings file may affect the next trusted session, so `sync` is an explicit configuration change, not a harmless preview.

Codex `hooks/list` verifies discovery with status **untrusted**. Local fake-provider CLI tests also verify that Codex skips the untrusted fixture and executes it with a one-invocation trust override, and that Claude executes the reverse-translated startup hook. These tests check event/source/cwd payload fields; they do not certify arbitrary script, output, timeout or failure semantics. See [native evidence](native-testing.md#local-fake-provider-startup-tests).

Native references: [Claude hook contract](https://code.claude.com/docs/en/hooks), [Codex hook contract](https://learn.chatgpt.com/docs/hooks).

## Example profile

Source builds also translate the same bounded definitions at a plugin's
conventional `hooks/hooks.json`. Use the Codex compatibility manifest layout;
portable root-manifest packages with hooks are rejected because the installed
Codex native test did not load their hooks. Hook path overrides, inline manifest
hooks, plugin-root interpolation and trust synchronization remain unsupported.

```json
{
  "version": 1,
  "stateDir": ".agent-bridge",
  "resources": [
    {"id":"guidance","kind":"instruction-file","scope":"project","portable":true,"claude":"sandbox/CLAUDE.md","codex":"sandbox/AGENTS.md"},
    {"id":"reviewer","kind":"agent-file","scope":"project","portable":true,"allowReformat":true,"claude":"sandbox/.claude/agents/reviewer.md","codex":"sandbox/.codex/agents/reviewer.toml"},
    {"id":"startup","kind":"hook-config","scope":"project","portable":true,"allowReformat":true,"claude":"sandbox/.claude/settings.json","codex":"sandbox/.codex/hooks.json"}
  ]
}
```

Prepare the sandbox sources, run `audit`, inspect `plan`, then explicitly `sync`. Agent/hook source packages bundled inside `plugin-directory` remain rejected: standalone adapter support does not automatically expand plugin support.
