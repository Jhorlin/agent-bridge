# Create and enroll a reviewed profile (source builds)

Prepare a runnable profile template with explicit `coordinationDir` and reviewed
resources. A `draft-profile` envelope is intentionally not runnable: first review
its candidates, extract/edit its `profile`, and make the required consent choices.
Then choose a new absolute target filename in an existing private directory:

```sh
agent-bridge review-enrollment /absolute/template.json /absolute/registered.json
agent-bridge create-enrolled /absolute/template.json /absolute/registered.json OBSERVATION
```

Review reads only. The observation binds template/inherited profile bytes,
effective configuration, managed inputs, existing roster and participant profile
bytes, plus the destination. Summaries describe the candidate's future sync plan;
enrollment itself never applies those native-file writes.

Creation holds the coordinator lock, checks overlap with all enrolled profiles,
and creates a private flattened profile with absolute paths. Inherited resources,
review consent and pinned links keep their effective meaning; the resulting file
does not keep an `extends` dependency. The original template stays unchanged.
The new file is registered in the coordinator roster. Existing or case-colliding
targets, missing parent directories, ownership overlaps, conflicts and stale
observations are rejected. Do not enroll a template and its clone against the same
native paths; the template is not automatically an active roster participant.

The two-file change is journaled, not globally atomic. Ordinary write failure
rolls it back, preserving later edits. Private snapshots remain under
`coordinationDir/enrollment-backups/<transaction>/create.json`. A pending marker
blocks cooperating current-version sync and enrollment commands until recovery.
No watcher, plugin installation, execution, authentication or trust grant occurs.

## Interrupted creation

Inspect the coordinator's `sync.lock`, `enrollment-pending.json` and referenced
journal privately. Confirm no writer remains before removing only a stale lock.
Then explicitly name the same destination and coordinator:

```sh
agent-bridge recover-enrollment /absolute/registered.json /absolute/coordinator
```

Recovery restores the prior roster and removes only the unchanged newly created
profile. Its content remains recoverable from the private journal. Native files
and sync state are not rolled back by this command. With no pending enrollment it
does nothing. Later edits, mismatched targets, unsafe paths or an ambiguous crash
between exclusive profile creation and recording ownership require manual
inspection; recovery will not guess which process created the file.

Exit 0 means success, 2 means a detected stale review, and 1 means another error
(including an occupied target or failed output). A reporting failure can happen
after commit. External editors and older binaries do not honor these locks or
markers; final check/write races and full power-loss durability remain outside
the guarantees. Keep profiles, rosters and journals private and trusted.
