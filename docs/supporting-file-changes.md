# Reviewed supporting-file renames and deletions

Source builds provide explicit file-change commands; ordinary synchronization
still refuses to infer deletions or renames.

```sh
agent-bridge review-file-change /absolute/profile.json demo/scripts/old.sh --rename scripts/new.sh
agent-bridge apply-file-change /absolute/profile.json OBSERVATION demo/scripts/old.sh --rename scripts/new.sh

agent-bridge review-file-change /absolute/profile.json demo/assets/obsolete.txt --delete
agent-bridge apply-file-change /absolute/profile.json OBSERVATION demo/assets/obsolete.txt --delete
```

Only raw supporting files under `scripts/`, `assets/` or `references/` in a
registered skill/plugin directory are eligible. Plugin skill supporting files
under `skills/NAME/scripts|assets|references/` are also eligible. All three copies
must exist with an established baseline, and the entire profile must be in sync
without conflicts or pending edits. Linked resources, entry points, manifests,
commands, hooks, MCP entries, whole resources and mixed host settings are excluded.

Review is read-only. The observation binds the choice, effective profile, current
managed inputs, baseline and rename destinations. Application acquires the normal
ownership/state locks and replans. Occupied/unsafe/overlapping destinations and
case-only renames are refused. Renames preserve each copy's existing file mode.

**References are not rewritten.** Inspect and update instructions/scripts that
refer to the old path; the bridge cannot infer their semantics. An explicit
deletion removes the selected file from all three managed copies and removes its
baseline entry. A rename creates the new copies, removes the old copies, and moves
that baseline. A subsequent ordinary sync must not resurrect the old path. Empty
directories may remain; no recursive directory removal is performed.

## Backups and interruption

Before writes, a private version-1 file-change journal is retained at
`stateDir/file-change-backups/TRANSACTION/journal.json`. It contains the original
bytes/modes and can contain sensitive data; never publish it. This is separate
from the unchanged version-1 normal synchronization journal and is not listed by
`history`. Completed file-change backups require private manual inspection for
historical restoration; automatic historical rename/delete undo is not provided.

Failures attempt exact rollback. Later edits block rollback rather than being
overwritten. An interrupted operation leaves `stateDir/file-change-pending.json`,
which blocks normal planning, syncing and normal recovery. Stop the watcher and
inspect any stale lock before running:

```sh
agent-bridge recover-file-change /absolute/profile.json
```

Recovery validates profile/resource identity, exact operation targets and the
baseline delta. New rename destinations are created exclusively; ownership is
journaled after creation. A crash between creation and its ownership receipt is
ambiguous: recovery refuses to delete that file and requires manual inspection.
Even a concurrently created file with identical bytes is not assumed to be owned.
Completed operations retain backups but remove the pending marker.

This is cooperative, guarded multi-file work, not a filesystem-wide atomic
transaction or power-loss guarantee. Stop older bridge binaries (which do not
know this pending marker) and external editors during changes. External edits
racing the final comparison are not fully fenced. Output failure after commit
does not undo the operation. Exit 2 identifies a digest mismatch; malformed or
newly conflicting/occupied/unsafe input may instead return exit 1. Review again
after resolving the cause rather than reusing an old observation.
