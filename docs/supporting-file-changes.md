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
`history`. Use `file-change-history` and the separate reviewed undo workflow below.

Failures attempt exact rollback. Later edits block rollback rather than being
overwritten. An interrupted operation leaves `stateDir/file-change-pending.json`,
which blocks normal planning, syncing and normal recovery. Stop the watcher and
inspect any stale lock before running:

```sh
agent-bridge recover-file-change /absolute/profile.json
```

Recovery validates profile/resource identity, exact operation targets and the
baseline delta. New rename/restoration destinations are created exclusively; ownership is
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

## Reviewed historical undo

```sh
agent-bridge file-change-history /absolute/profile.json
agent-bridge review-file-change-undo /absolute/profile.json TRANSACTION
agent-bridge apply-file-change-undo /absolute/profile.json OBSERVATION TRANSACTION
```

History lists retained transaction IDs, item keys, rename choices and whether an
entry restores a deleted file; it never prints file contents. These are retained
attempts, including failed/rolled-back attempts, not proof of committed events.
Malformed or no-longer-owned historical resources block history inspection.

Undo requires a conflict-free, fully synchronized current profile, unchanged
resource identity and the selected operation's native after-state. The original
path must still be absent. For a rename, its destination must still contain the
exact recorded bytes and mode on each side; undo restores the old paths and
removes those unchanged destinations. For a deletion, undo recreates each copy
with its own recorded bytes/mode. Case collisions, symlinks, occupied targets and
newer edits are refused. Even equivalent-but-differently-formatted files are not
silently overwritten.

Review pins the selected journal bytes, transaction ID, current raw inputs and
new transaction. Apply rechecks under the normal locks. Only the selected item's
baseline is restored/moved: newer unrelated files and baselines remain intact.
Undo is another journaled operation with the same rollback/recovery and exclusive
creation receipts described above. It does not delete the original backup or
rewrite references. A successful undo cannot be repeated against that old
after-state. Restoration journals cannot themselves be undone implicitly; review
a fresh explicit deletion if you decide to remove the restored file again.

Deletion-restoration journals use an optional `restore` field in the separate
version-1 file-change journal. Older binaries reject that journal and cannot
recover it; retain the binary used for the operation. The ordinary sync journal,
manifest and configuration schema versions remain unchanged. All historical
snapshots remain sensitive local data, not authenticated or signed receipts.

Tests cover read-only history/review, rename/delete undo, exact modes, newer
unrelated state, repeated-undo refusal, stale journal/input, path/identity checks,
every-write rollback, interrupted recovery, later-edit and ambiguous-ownership
preservation, and CLI output privacy. Whole-resource retirement is not included.
