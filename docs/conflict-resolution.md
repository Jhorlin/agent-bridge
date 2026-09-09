# Reviewed conflict resolution (source builds)

Normal sync and watch never pick a winner for concurrent edits or deletions.
To resolve a conflict, inspect the contents privately, choose a side for every
conflicted item, then preview:

```sh
agent-bridge review-resolution /absolute/profile.json rules=claude demo/SKILL.md=codex
```

Item keys are the resource ID for single files, `ID/relative/path` for directory
entries, and `ID/@manifest` for plugin manifests. Choices are `claude`, `codex`
or `shared`. A choice applies to the entire selected item: for MCP this means
all selected servers in that resource, not just the conflicting server.

The JSON result contains summaries and an observation digest, but no file
contents. Review **all** proposed writes: ordinary non-conflicting pending changes
in the same profile are included. Then use the returned digest and exact choices:

```sh
agent-bridge resolve-reviewed /absolute/profile.json OBSERVATION rules=claude demo/SKILL.md=codex
```

The digest binds the choices, effective profile, raw managed inputs and manifest.
Application rechecks it under the usual ownership/state locks. Changed inputs or
choices return exit 2; other failures return exit 1. Re-review after a change.
Successful application updates baselines through the normal guarded transaction.
Overwritten bytes are retained in its private journal; write failures use the
same rollback and later-edit protection as ordinary sync. Do not publish backups.

Every conflict needs an explicit choice. Unknown keys, duplicate CLI choices,
nonconflicted items, unsupported adapter data and absent selected sides are
rejected. Selecting an existing side can restore a deleted peer; selecting an
absent side does **not** propagate deletion. Renames, deliberate deletion and
historical restore are not provided by these conflict commands; the separate
historical selection workflow is described below.

This is not a three-way text editor or proof of behavioral equivalence. Inspect
instruction/skill/script contents before selecting them. No installation,
execution, authentication or trust grant occurs. External editors remain outside
the bridge's locks, so its documented final check/write race still applies.

## Selecting a retained historical version

```sh
agent-bridge history /absolute/profile.json
agent-bridge review-history /absolute/profile.json TRANSACTION rules codex after
agent-bridge restore-reviewed /absolute/profile.json OBSERVATION TRANSACTION rules codex after
```

`history` lists retained journal IDs and operation counts, sorted by ID, **not
chronologically**. It is not a commit log: a retained journal can describe a
successful, rolled-back or interrupted attempt. Inspect journals privately when
choosing an ID. No raw paths, snapshots or contents are printed by these commands.

Choose one currently registered item, its recorded side and `before` or `after`.
Only versions actually recorded as write operations are available. An unchanged
source may therefore have no entry in that transaction. Absent snapshots, changed
resource identity, unsupported historical content, unsafe journals, other current
conflicts and pending transactions are rejected. Restore never runs old code.

Review binds both the current inputs and the exact journal bytes. The write
command rechecks that observation under locks, then runs the selected content
through today's adapters and the normal transaction engine. Current host-local
instruction overlays/settings survive; this restores the selected portable
content, **not** every historical byte or the old manifest. Other non-conflicting
pending profile changes are included in the preview. A new journal backs up the
current versions; the original historical journal remains untouched.

The same exit codes and external-editor race limits apply. This does not prune
files, recreate an unregistered resource, propagate a historical absence, or
automatically infer a rename. Keep historical state private and trusted.
