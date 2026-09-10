# Convention-based project and global synchronization

Current source builds discover all six supported categories: instructions, skills,
agents, hooks, MCP definitions, and plugin authoring packages. Select a root once;
new components are discovered on later loads and watcher polls. No per-component
entries or shared-section markers are needed. This does **not** make every native
feature portable or install/enable plugins. The published `v0.1.0-alpha.1` binary
does not include this feature.

## Project setup

Build current Go source, then use the resulting executable:

```sh
agent-bridge init /absolute/project/.agent-bridge.json --conventions
agent-bridge config /absolute/project/.agent-bridge.json
agent-bridge audit /absolute/project/.agent-bridge.json
agent-bridge plan /absolute/project/.agent-bridge.json
# Review contents, exclusions, formatting changes and native settings first:
agent-bridge sync /absolute/project/.agent-bridge.json
agent-bridge watch /absolute/project/.agent-bridge.json --apply
```

`init` creates a private profile only, refuses overwrite, and does not sync or start
a service. Its parent must exist. Ignore the profile and `.agent-bridge/` state in
Git. Try temporary fixtures first. Mutating commands produce private diagnostics.

The generated project profile is equivalent to:

```json
{
  "version": 1,
  "stateDir": ".agent-bridge",
  "conventions": {
    "root": ".",
    "scope": "project",
    "features": ["instructions", "skills", "agents", "hooks", "mcp", "plugins"]
  },
  "resources": []
}
```

`root` is profile-relative or absolute and must be an existing safe directory, not
a filesystem root or private state/coordinator directory. Project discovery walks
ordinary nested directories within that root, stopping at the boundaries below.

## Global setup

Use a separate profile with an **explicit** home root; the bridge does not infer it
from the shell or search other users' homes:

```sh
agent-bridge init /absolute/profiles/global.json --global /absolute/home
agent-bridge audit /absolute/profiles/global.json
agent-bridge plan /absolute/profiles/global.json
# After review:
agent-bridge sync /absolute/profiles/global.json
agent-bridge watch /absolute/profiles/global.json --apply
```

This creates the same policy with `scope: "global"` and the selected absolute root.
Global mode examines only the known locations below: it **does not recursively
scan the home, find projects, or traverse installed plugin caches**. Global files
apply according to each host's own loading rules. Run separate project profiles
where project-specific definitions are needed; global mode does not adopt them.

## Locations and translation

Project paths are relative to each discovered project directory. Global paths are
relative to the selected home root, without recursive traversal.

| Feature | Claude location | Codex location | Behavior |
| --- | --- | --- | --- |
| Project instructions | `CLAUDE.md` | `AGENTS.md` | Whole-file pairs, including nested directories |
| Global instructions | `.claude/CLAUDE.md` | `.codex/AGENTS.md` | One global whole-file pair |
| Skills | `.claude/skills/NAME/` | `.agents/skills/NAME/` | Strict metadata, supporting files, invocation policy |
| Agents | `.claude/agents/NAME.md` | `.codex/agents/NAME.toml` | Name, description, body; bounded settings stay local |
| Hooks | `.claude/settings.json` | `.codex/hooks.json` | Supported hook subset; unrelated settings preserved |
| Project MCP | `.mcp.json` | `.codex/config.toml` | Discover names; translate portable definitions |
| Global MCP | `.claude.json` | `.codex/config.toml` | Top-level user MCP only |
| Plugin authoring | `.agent-bridge-plugins/claude/NAME/` | `.agent-bridge-plugins/codex/NAME/` | Supported package components; installation separate |

The plugin directories are **Agent Bridge's authoring convention**, not native
installed-plugin locations. Place reviewed source packages there. Existing Codex
root `plugin.json` selects portable layout; otherwise compatibility layout uses
`.codex-plugin/plugin.json`. Ambiguous dual manifests fail. Portable-layout hooks,
MCP and commands remain unsupported. Claude package agents export to namespaced
standalone `.codex/agents/bridge-RESOURCE_ID-NAME.toml` files, never inside the Codex
package. Later agents and MCP names are adopted without profile edits. Reverse
edits to managed exports update the Claude authoring package. Native install,
refresh, enablement and trust remain separate. Uninstall does not remove exports.

Automatic profiles select strict adapters and consent to JSON/TOML/YAML
reformatting. Skills translate `disable-model-invocation` to the inverse Codex
`policy.allow_implicit_invocation`. Pre-existing Codex skills without that sidecar
use default implicit invocation; removing a tracked sidecar blocks synchronization.
Bounded agent models/tools/permissions and Codex MCP policy fields stay in their
original host, not the shared model or opposite host. New destinations use native
defaults: review permissions independently. See [adapter limits](adapters.md),
[skills](skill-invocation.md), [agents/hooks](portable-adapters.md), and
[plugin exports](plugin-agent-exports.md).

Codex project configuration also requires native project trust. An isolated Codex
0.153.4 test found the generated project hook only after trusting the disposable
project; its hook still remained untrusted. Project trust and hook trust are
separate native decisions, and the bridge grants neither. See the
[Codex hook contract](https://learn.chatgpt.com/docs/hooks).

## Ongoing reconciliation and safety

Discovery runs on every profile load; watch reloads once per polling cycle. New
components need no restart. Two stable observations and an under-lock freshness
check precede watcher writes. Inventory changes invalidate review tokens. Project
scan cost grows with the selected tree.

Stable derived IDs use the ordinary baseline/journal/recovery engine. Independent
file changes merge; differing edits to the same shared content block the entire
transaction. New MCP names on either side merge independently, including disjoint
initial sets. Different definitions of the same name conflict. Tracked missing
files/server entries, including disappearance on both sides, remain conflicts.
Renames/deletions require reconciliation, not last-writer-wins. Reviewed retirement
preserves files and records exclusions against immediate re-adoption.

Auto-managed MCP allowlists and plugin exports can grow while existing members
remain pinned. Historical restore across a change to those member lists is
deliberately rejected by the strict historical identity check; use a matching
post-growth snapshot or review/reapply selected content. Recovery refuses
incompatible identities or later edits instead of overwriting them. Keep original
profiles/state and stop other editors while recovering an interrupted operation.

Global Claude `.claude.json` mixes account state, trust and per-project MCP data.
Only top-level `mcpServers` definitions participate; nested project entries and
account/authentication data are not copied to Codex. Transactional snapshots back
up the **original mixed file**. Keep recovery state private; never upload backups.
Use sanitized support bundles instead. Literal credential values in supported MCP
env/header fields are rejected; use references. This is not a general secret
scanner: review arbitrary bodies, scripts and arguments before sharing.

## Boundaries and exceptions

Ordinary traversal skips hidden directories, `node_modules`, `vendor`, `dist`,
`build`, `coverage`, `target`, `__pycache__`, `venv`, state/coordinator directories,
and nested repositories containing `.git`. Known hidden component locations above
are inspected explicitly. Installed caches, histories, automatic memories,
credentials, marketplace enablement and arbitrary settings are not enrolled.
Custom config roots, legacy skill locations and admin directories require explicit
exceptions; discovery does not infer environment overrides.

Use root-relative file/subtree exclusions, without globs:

```json
"conventions": {
  "root": ".",
  "scope": "project",
  "features": ["instructions", "skills", "agents", "hooks", "mcp", "plugins"],
  "exclude": ["content-sources", "generated", ".claude/skills/private"]
}
```

`.gitignore` is not interpreted. Excluding either native member excludes the whole
automatic component. Excluded collection members are skipped before content reads.
Collection-level regular `README.md`, `LICENSE`, and `LICENSE.md` files are
ignored, not treated as components or copied. Other unexpected collection files
still block discovery. Names must be safe kebab-case (at most 64 characters).
Unsafe links and unsupported
collection entries fail; unrelated symlinks are not traversed. Explicit pinned-root
symlinks remain an opt-in exception.

- `CLAUDE.local.md` stays private and produces a fixed warning in project mode.
- Project `.claude/CLAUDE.md` is discovered as an
  [instruction set](instruction-sets.md), composing it with any same-directory
  `CLAUDE.md` while preserving the separate sources. Adding an alternate to an
  already tracked plain pair requires reviewed adoption in a fresh state directory.
  Project/global `AGENTS.override.md` still requires explicit handling or exclusion.
- Automatic instructions containing whitespace-delimited `@references` outside
  closed Markdown fences and matched single-line code spans block planning.
  Literal package names in code examples do not count as imports. Unclosed fences
  require review. Other Markdown forms remain conservatively handled; imports are
  never expanded. See [Claude's import contract](https://code.claude.com/docs/en/memory).
- Automatic skill entries containing recognized `$ARGUMENTS`, numeric argument
  placeholders, `${CLAUDE_*}` variables or dynamic-shell syntax block writes,
  including literal examples. Explicit resources remain available after review.
- Existing `.claude/rules` directories produce a fixed warning: path-scoped rules
  are not translated and must be reviewed independently in Codex.
- Unsupported skill metadata, agent fields, hooks, MCP transports and plugin
  components block writes. Placement is not portability certification.
- Nested placement does not guarantee identical native discovery, inheritance,
  precedence or behavior. Verify each intended native session separately.

## Existing profiles and compatibility

Existing explicit profiles are unchanged. Older `conventions: {"root":"."}`
policies remain **instruction-only**: upgrading never silently broadens an approved
profile. Opt into all categories by adding the feature list above, then audit and
plan. A nonempty subset can enable fewer categories.

Exact explicit native pairs take precedence and retain IDs/baselines. Other
overlaps fail; exclude the automatic pair before supplying a different mapping.
Keep existing explicit resources when adopting conventions to preserve history.
Conventions belong only in a leaf profile, never an `extends` parent. Enrollment
retains the policy instead of freezing inventory into static resources. Do not run
overlapping profiles without [shared coordination](onboarding.md#multiple-profiles).
Config remains schema 1, manifest 2, journal 1. Do not downgrade an adopted profile
to an executable without these automatic identities.

## Evidence and native contracts

Isolated tests cover six-category project/global sync, nested/reverse sources,
watcher adoption, growing MCP/plugin members, local-data preservation, conflicts,
deletions, stale review, rollback, interrupted recovery, retirement, policy
companions, layouts, exclusions and global cache/home boundaries. They do not
prove authenticated execution or equal security enforcement.

Targeted native checks passed with Claude Code 2.1.267 and Codex 0.153.4 in
disposable homes: MCP configuration readers, skill invocation policy, plugin-agent
exports, and skill/hook discovery. The added project-convention check verifies
actual Codex discovery with the project-trust boundary. Execution probes use a
local fake provider, not paid model calls or real account credentials.

Locations follow [Codex skills](https://learn.chatgpt.com/docs/build-skills),
[Codex agents](https://learn.chatgpt.com/docs/agent-configuration/subagents),
[Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli),
[Claude settings](https://code.claude.com/docs/en/settings) and
[Claude plugin packages](https://code.claude.com/docs/en/plugins-reference).
The authoring staging directory is a bridge convention, not a host standard.
For a staged rollout with native exceptions, see [complex monorepos](monorepos.md).
