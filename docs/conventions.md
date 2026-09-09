# Convention-based project instructions

Current source builds synchronize ordinary `CLAUDE.md` ↔ `AGENTS.md` pairs
throughout one selected project. No per-file entries or shared-section markers
are necessary. This is **instruction-file discovery**, not automatic sharing of
all skills, MCP servers, plugins, credentials, global settings, or host behavior.
The published `v0.1.0-alpha.1` binary does not include this feature.

## Start with a project

Build the current Go source, then use the resulting executable:

```sh
agent-bridge init /absolute/project/.agent-bridge.json --conventions
agent-bridge config /absolute/project/.agent-bridge.json
agent-bridge audit /absolute/project/.agent-bridge.json
agent-bridge plan /absolute/project/.agent-bridge.json
# After reviewing inventory, contents, exclusions, and the plan:
agent-bridge sync /absolute/project/.agent-bridge.json
agent-bridge watch /absolute/project/.agent-bridge.json --apply
```

`init --conventions` creates only a new private profile, refuses overwrite, and
selects the directory containing that profile. It does not sync or start a
service. Ignore the local profile and its `.agent-bridge/` state directory in Git.
Use temporary fixtures for an initial trial. Mutating CLI commands produce the
same private diagnostics as other profiles.

The generated profile is equivalent to:

```json
{
  "version": 1,
  "stateDir": ".agent-bridge",
  "conventions": { "root": "." },
  "resources": []
}
```

`root` is profile-relative or absolute. It must be an existing safe directory,
not a filesystem root or a directory inside the profile's state/coordinator.
Only this root is selected; no home-directory or other-project discovery occurs.

## Ongoing behavior

| Found file | Matching peer |
| --- | --- |
| `CLAUDE.md` | `AGENTS.md` |
| `src/CLAUDE.md` | `src/AGENTS.md` |
| `src/api/AGENTS.md` | `src/api/CLAUDE.md` |

Discovery checks both sides on each profile load. The watcher reloads once per
polling cycle, so new pairs do not require profile edits or a watcher restart.
Two stable observations and the under-lock freshness check are still required.
Scan costs grow with the non-excluded project tree.

Each directory receives a stable hashed resource ID, visible with `config` or
`plan`, and an independent baseline. Whole-file bytes are preserved. Nothing is
flattened or moved. Existing differing peers and concurrent differing edits block
the whole transaction. Deletions—including deletion of **both** files—remain
conflicts, not silent removal from tracking. Renames require reconciliation.
Use reviewed retirement to stop managing a pair without deleting either file;
retirement records exclusions so it cannot be immediately rediscovered.

New pairs enter the ordinary journal/backup/recovery engine. Pending first-sync
journals retain derived identities for recovery even if source files vanish.
Manifest paths must match reconstructed conventional targets within the root.

## Boundaries and exceptions

Default scanning excludes hidden directories, `node_modules`, `vendor`, `dist`,
`build`, `coverage`, `target`, `__pycache__`, `venv`, and the profile's state and
coordination directories. A nested directory containing a `.git` file or directory
is a separate repository boundary. Unrelated symlinks are not followed; instruction
symlinks, hardlinks, non-regular files, and case-variant names are rejected.

Add root-relative file or subtree exclusions, with no globs:

```json
"conventions": {
  "root": ".",
  "exclude": ["content-sources", "generated", "examples/foreign-project"]
}
```

`.gitignore` is **not** interpreted. Add private or custom generated directories
to `exclude`; filenames alone do not prove content is safe or portable. Excluding
either member excludes its entire automatic pair, leaving existing files and
baselines untouched. Making an already tracked directory a new repository or
other default boundary blocks loading until explicitly excluded or restored.

Special cases are not silently translated:

- `CLAUDE.local.md` stays private/host-local and is skipped with a fixed warning
  in `config`, audit, and normal plan/sync/watch output.
- `.claude/CLAUDE.md` and `AGENTS.override.md` require explicit handling or
  exclusion. They otherwise block discovery; an override may shadow `AGENTS.md`.
- Automatic pairs containing whitespace-delimited `@references` block planning
  conservatively. This includes some mentions or code examples, not only actual
  Claude imports. Review and use an explicit resource exception, or remove/replace
  the host-specific reference. Imports are not expanded.
- Matching filenames do not guarantee identical instruction loading, inheritance,
  precedence, or agent behavior. Verify each host separately.

Explicit resources remain available for exceptions. An exact explicit
`CLAUDE.md`/`AGENTS.md` pair takes precedence and retains its existing resource ID
and baseline. Other overlapping mappings are rejected; exclude the automatic pair
before supplying a different mapping. Existing explicit profiles remain unchanged
unless `conventions` is added. Keep any existing root resource when adopting this
feature, so its baseline and history are retained.

Declare `conventions` only in a leaf profile. A convention-enabled profile cannot
be used as an `extends` parent; global explicit resources can still be inherited
by a convention-enabled project. Reviewed enrollment preserves the convention
policy rather than freezing the inventory into explicit pairs. Do not run
overlapping profiles concurrently without the shared coordination roster
described in [onboarding](onboarding.md).

## Verification

Regression tests cover nested and reverse discovery, new-file watcher adoption,
initial conflicts, deleted pairs, stable inventory review, write rollback,
interrupted first-sync recovery, explicit-profile migration, retirement,
flattening, exclusions, repository boundaries, unsafe links, imports/overrides,
and forged tracked paths. These are isolated filesystem tests, not authenticated
model calls or proof that both hosts interpret every instruction identically.
