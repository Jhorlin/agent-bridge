# Explicit plugin agent exports

Codex 0.153.4 did not discover agents inside an installed compatibility plugin in
our isolated native test. Source builds offer an explicit alternative: map each
reviewed Claude plugin agent to a **standalone Codex agent file**, not to an
unsupported package component. The official [Codex custom-agent contract](https://learn.chatgpt.com/docs/agent-configuration/subagents)
documents standalone personal/project TOML files and their required fields.

Add this resource to a new version-1 profile, using your own explicit paths:

```json
{
  "id": "bundle",
  "kind": "plugin-directory",
  "scope": "global",
  "portable": true,
  "allowReformat": true,
  "claude": "/absolute/authoring/claude-plugin",
  "codex": "/absolute/authoring/codex-plugin",
  "codexAgentExports": {
    "reviewer": "/absolute/codex-home/agents/bridge-bundle-reviewer.toml"
  }
}
```

The selected Claude file is `agents/reviewer.md`. Only frontmatter `name` and
`description`, plus the instruction body, are mapped. The source name must be
`reviewer`; Codex receives `bridge-bundle-reviewer`. In general its name/filename
is `bridge-RESOURCE_ID-SOURCE_NAME[.toml]`. Namespacing avoids built-in names and
reduces collisions, but cannot certify the absence of unrelated custom agents
with duplicate names elsewhere. Review global/project precedence yourself.

Export paths may be config-relative or absolute; inherited relative paths belong
to the declaring profile. Select the actual standalone directory for the intended
Codex scope. The bridge does not discover or alter the host's agent directory
automatically. Project-scoped exports work through explicitly selected project
paths. New native sessions may be needed to observe changed definitions.

Every package agent must be individually listed. Unlisted/nested agent files,
wrong names, unsupported metadata, plugin-root macros,
symlinks and overlapping exports are rejected. Exports cannot overlap package
roots, other managed paths, profile/state/coordinator files or service storage.
Plugin resources with exports cannot use linked roots. Other agent orchestration
features, relative references and script behavior are not translated.

By default host-local model/tool/permission fields are rejected. A separate
`"preserveAgentSettings": true` opt-in permits the same bounded local fields as
the standalone agent adapter: Claude model, tools/disallowedTools, permissionMode
and maxTurns; Codex model, model_reasoning_effort, sandbox_mode and approval_policy.
They stay in their original native file, including during historical restore;
none are copied into the other host or the shared canonical definition. A newly
created destination has no translated local settings and uses that host's defaults.
Configure/review its model and permissions independently before use. Retention
does not certify equal enforcement or every value's support in every host version.

## Synchronization and ownership

The shared entry `RESOURCE_ID/agents/NAME.md` uses the canonical agent JSON model;
Claude receives Markdown and the explicit external Codex path receives TOML.
No agent file is written inside the Codex plugin package. Reverse edits to the
standalone file update Claude's authoring package, restoring its original short
agent name. References to agent names inside instruction bodies are not rewritten.
Review bodies for both hosts; discovery alone does not prove equal execution.

Explicit export paths are included in profile identity, inheritance/enrollment,
ownership overlap checks, stale review, conflict selection, history, journal
target validation and rollback. Ordinary deletion remains a conflict, not an
implicit uninstall. Adding/changing exports changes resource identity: retain
old profiles/state, stop their writers and review adoption into a new
non-overlapping profile/state. Do not erase baselines to bypass that check.

## Separate lifecycle

Claude still loads the agent through its installed plugin, so authoring changes
require a native install/refresh. Codex loads the external standalone definition
independently of plugin enablement/installation. **Uninstalling a Codex plugin
does not remove its standalone exports.** This bridge does not pretend those
two lifecycles are equivalent or use production-unsupported install APIs.

`compare-plugin-copy` compares package copies, excluding external exports; its
warning calls out that distinction. Stop the relevant watcher and review native
agent availability independently when retiring a package. Automatic whole-resource
retirement remains unsupported. No native approvals, credentials or permissions
are copied, and no model call runs merely because you sync a definition.

## Evidence

Offline tests cover forward/reverse fields and names, repeat sync, conflict and
history selection, raw-review staleness, deletion/metadata/path rejection,
inheritance/enrollment, ownership/service collisions, injected rollback at each
write and later-edit recovery refusal. Accepted definitions are round-trip fuzzed.

The pinned, Apache-2.0 [upstream corpus](../internal/bridge/testdata/upstream/README.md)
includes Anthropic's unchanged code-simplifier agent as inert data. Tests verify
rejection without settings consent, retention of its Claude-only `model: opus`,
forward/reverse edited fixture copies and independent Codex settings. Its actual
instructions never execute; synthetic fixed-provider tests supply native evidence.

Native tests use Claude Code 2.1.266 and Codex 0.153.4, disposable homes and a
local fake provider. They verify discovery of the bridge-generated Codex export
and Claude discovery after reverse sync and fixture plugin installation. The
negative in-package Codex agent test remains in place. These are loading tests,
not paid model calls, arbitrary agent execution or permission-equivalence proofs.
