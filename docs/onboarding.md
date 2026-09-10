# Global and project onboarding

Before global skill enrollment, run the separate
[duplicate and native-variant inventory](skill-inventory.md). Preserve already
shared links and installer-owned variants; do not treat cache matches as active
installations or missing entries as proof of portability.

For ongoing project/global component discovery, use [convention-based setup](conventions.md).
`init PROFILE --conventions` selects the containing project and all six supported
categories; `init PROFILE --global ROOT` selects known global locations under an
explicit root without traversing the home. The manual workflow below remains
available for tighter selection and nonstandard-path exceptions.

Discovery is read-only and opt-in per root. It inventories filenames and metadata,
not native file contents. It does not import, enroll, approve, or execute anything.

```sh
agent-bridge discover /absolute/home --global
agent-bridge discover /absolute/project --project
agent-bridge init /absolute/profiles/bridge.json
```

The profile parent directory must already exist. `init` creates a private (0600),
empty profile and refuses to overwrite an existing file. It does not create host
settings or synchronization state. In current source builds, `init` and other
mutating commands also produce private rotating [diagnostic events](diagnostics.md)
outside managed state. Discovery, audit, plan and preview watch remain read-only.

Discovery looks for conventional instruction files, skill directories, individual
agent files and hook settings. Project discovery also offers MCP configuration.
This filename-only `discover` command excludes global MCP because Claude's
`.claude.json` mixes account and trust state. The separate all-feature convention
policy explicitly supports its top-level MCP definitions with local-data preservation
and a warning that private transaction backups include the original mixed file. Installed plugin caches, credentials, histories,
automatic memories, and unregistered project trees are not scanned. Custom host
configuration roots require manually specified profile paths. Symlink candidates
are rejected, not followed.

Candidates are suggestions, not a runnable profile. A settings/config filename
does not prove it contains portable hooks or MCP servers, and skill directories
are not validated during discovery. Review contents, copy selected candidates into
the profile's `resources` array, and provide the required explicit portability,
reformatting consent and MCP server allowlist. See [adapter requirements](adapters.md)
and [instructions, agents and hooks](portable-adapters.md).

Instruction candidates default to whole-file sharing. Choose `instruction-file`
and prepare shared markers if host-specific instructions must remain separate.
Never enroll credentials, tokens or host-specific security settings.

Run `agent-bridge audit PROFILE --json` and `agent-bridge plan PROFILE` before
`agent-bridge sync PROFILE`. Differing initial copies require reconciliation;
discovery never chooses a winner. After a successful sync, `watch PROFILE` previews
changes and `watch PROFILE --apply` applies non-conflicting changes while running.
There is no background service installed automatically.

If a profile or structured input is temporarily incomplete while being saved,
watch mode pauses writes and retries once per second. It reports the pause once,
then reports resumption when planning succeeds. Unsupported input and pending
recovery also remain blocked; retries do not grant consent or perform recovery.
Transaction errors, including lock contention during apply, still stop the watcher.

Watch application requires two consecutive identical observations of all raw
inputs and the resolved profile. Read/parse errors reset that stability check.
After acquiring the write lock, the bridge rejects a changed observation before
creating a transaction journal or changing native files. This reduces propagation
of intermediate editor saves; it cannot prove an editor has finished or eliminate
the documented external-editor check/write race. Explicit `sync` remains immediate.

## Multiple profiles

Set the same absolute `coordinationDir` in every profile that can write overlapping
files, or inherit it from a common base profile:

```json
{
  "version": 1,
  "stateDir": "./project-state",
  "coordinationDir": "/absolute/private/agent-bridge-coordination",
  "resources": []
}
```

Relative coordination paths resolve against their declaring profile, including
inherited paths. The coordination directory must not overlap profile files,
resource roots or the profile's state directory. Sync/recovery acquires its common
lock first, then the profile state lock. Contention returns an error without
entering the transaction; it does not queue, retry or steal a lock. Read-only
commands do not create coordination directories or acquire locks.

This is opt-in serialization, not shared baselines, profile discovery or permanent
resource ownership. All participating profiles must use the same coordination
directory; unrelated tools and profiles using a different directory are not
protected. Watchers stop on contention and need restarting after inspection.
Use one writer profile for each shared resource wherever possible.

After an interrupted process, a `sync.lock` may remain in either directory.
Inspect both locks and ensure no writer is running before manually removing an
exact stale lock. Then run recovery. The program never treats PID reuse or lock
age as permission to delete a lock.
