# Logs, troubleshooting and safe support bundles

These commands are available in **current Go source builds**, not the immutable
`v0.1.0-alpha.1` binaries. Build with `go build -o agent-bridge ./cmd/agent-bridge`.
No extra runtime, model call, account connection or telemetry server is involved.

## When something goes wrong

Use the same explicit profile path and OS user that ran the failing command:

```sh
./agent-bridge doctor /absolute/path/bridge.json
./agent-bridge logs /absolute/path/bridge.json --tail 100
./agent-bridge support-bundle /absolute/path/bridge.json /absolute/path/new-support.json
```

1. `doctor` emits a sanitized JSON report. Check `config`, `plan`, `state`,
   `service`, and recent `logs.events`. It does not fix anything automatically.
2. `logs` shows the structured log directory, recent validated events, legacy
   service-log locations, and a **local-only reference map**. This maps hashed
   resource/path references back to the current profile's IDs and files, including
   the manifest, pending markers and lock paths. It does
   not print native file contents. Treat this output as private: it includes paths
   and names, unlike a support bundle.
3. `support-bundle` writes one new private JSON file. Choose an existing directory
   outside the profile, inherited configs, managed resources, state, coordinator
   and diagnostic directory. The output must not already exist. Review it before
   attaching it to an issue or giving it to an assistant. Nothing is uploaded.

An assistant working locally can use `doctor` and `logs` to correlate an error with
code and files. For remote review, share the sanitized bundle, the command name
and what you expected—not credentials, an entire home directory or raw backups.

`doctor` and `logs` are read-only. They can load the selected profile/native files
to check planning and query an **already owned** service's status. They never
start/stop services, execute native agent tools, run hooks, authenticate, acquire
sync locks, recover transactions or create logs. Their exit 0 means the report was
produced, **not** that every reported check passed. Usage/output errors exit 1.
`logs` defaults to 50 events; `--tail` accepts 1–1000. Bundles retain at most the
newest 200 validated events available in the bounded logs.

## Where logs live and when they are written

The directory key is the full lowercase SHA-256 of the cleaned absolute profile
path. Use `logs PROFILE` to discover it rather than calculating it manually.

| Platform | Structured log directory |
|---|---|
| macOS | `$HOME/Library/Logs/agent-bridge/<profile-hash>/` |
| Linux | `$XDG_STATE_HOME/agent-bridge/<profile-hash>/`, or `$HOME/.local/state/agent-bridge/<profile-hash>/` when unset |

`XDG_STATE_HOME`, when used, must be absolute. macOS uses its standard log location
and ignores that variable. Moving the profile, running as another user, or changing
Linux's state root changes the lookup location; old logs are not migrated.

The executable records attempts for `sync`, `sync-reviewed`, `recover`, `init`,
`enroll-reviewed`, `create-enrolled`, `recover-enrollment`, `resolve-reviewed`,
`restore-reviewed`, applying retirement/supporting-file changes and undo,
`recover-file-change`, and service install/start/stop/uninstall. `watch --apply`
records its lifecycle, conflicts, errors, and actual transactions. Creation uses
the **target profile**, not the enrollment template, as the log identity.

Preview commands—including `plan`, `audit`, reviews, `config`, discovery,
`service status`, and `watch` without `--apply`—do not create persistent structured
logs. Redirected CLI output and native service stdout/stderr are separate streams.
Library callers of `cli.Run` remain non-persistent; the executable uses
`cli.RunLogged`. Bridge APIs accept optional content-free observers.

New directories use mode 0700; event files and `log.lock` use 0600. Existing log
directories/files must be private and owned by the current user. Symbolic links,
hard-linked event/lock files and unsafe ancestors are rejected, not repaired.
Logging is outside synchronized state and is disabled when a valid profile
overlaps that location, including a reloaded applying-watch profile. Do not enroll
the diagnostic directory as a managed resource.

## Retention and failure behavior

`events.jsonl` holds current records. Rotation retains `events.1.jsonl` (newest
archive) through `events.3.jsonl` (oldest), each at most **1 MiB** for bridge-written
logs: **4 MiB of event data per profile**. Filesystem allocation and the empty lock
file are additional overhead. Rotation removes the oldest archive automatically;
those aged-out records are not recoverable unless separately saved. This limit
does not bound raw native service logs, backups, the number of profiles, or files
externally modified by other software.

A per-process mutex and non-blocking OS file lock serialize writes/rotation.
`log.lock` is a persistent **advisory lock file**: its presence does not mean a
logger is running. The OS releases the lock on process exit. It is unrelated to
the bridge's fail-closed `sync.lock`; never apply log-lock assumptions to sync
recovery locks.

Identical consecutive events are limited to once a minute, with transitions and
distinct target writes retained. An idle healthy applying watcher does not log
every polling cycle. This is not a heartbeat or a complete event-delivery system.

If storage is full, permissions are unsafe, another logger holds the lock, or a
write/rotation fails, logging issues one fixed warning and disables itself for
that run. **Sync and recovery retain their normal behavior.** Logging never rolls
back a completed sync or changes its exit code. Resolve the logging problem and
start a new run to resume recording. A failure can leave a partial record, and a
kill/crash may leave no `command_finish`. Readers omit invalid records and report
rejections/unavailable files; rotation and live reads are not an atomic snapshot.

Console warnings use `log_path_unsafe`, `log_init_failed` or `log_write_failed`.
`logs.status: ok` means available records could be read; it is **not** a writable
disk probe or proof that no records were lost. A read-only doctor does not test
disk writes. Logging is best-effort troubleshooting, not a tamper-proof audit log,
power-loss guarantee or security boundary against another process with your UID.

## Understanding an event

Every persisted event has schema 1, UTC `time`, `level`, build `version`, a random
`run` ID, hashed `profileRef`, a fixed command name, `stage`, `component`, and
stable `code`. Optional fields identify hashed `resourceRef`/`pathRef`, a validated
transaction UUID, and command duration/exit status. Exit 0 is omitted in JSON;
the completion code is `ok`. Exit 2 is summarized as `blocked`; inspect earlier
events for the conflict or stale observation. Build VCS `revision` and `dirty`
are included when embedded by Go. Reproducible release packaging deliberately
disables VCS metadata and identifies the build by its release version instead.

| Component / stage | Where to investigate |
|---|---|
| `cli` / `config_load` | Selected profile, schema, inheritance and safe paths; `internal/cli/cli.go` |
| `bridge.plan` / `expand`, `read`, `normalize` | Resource/path reference and adapter input; `internal/bridge/plan.go` dispatches to the adapter |
| `bridge.transaction` / `lock`, `prepare`, `render`, `validate` | Ownership, freshness, rendering or round-trip validation; `internal/bridge/transaction.go` |
| `bridge.transaction` / `journal`, `write`, `receipt`, `commit`, `rollback` | Transaction UUID, target reference, pending state and ownership receipts; `internal/bridge/transaction.go` |
| `bridge.recovery` / `recover` | Validated transaction and recovery result; `RecoverObserved` in `internal/bridge/transaction.go` |
| `cli.service`, `cli.enrollment`, `cli.file_change`, `cli.history`, `cli.resolve` / `operation` | Corresponding command handler under `internal/cli/`; reviewed choices and its private recovery state |

Detailed stage/target events cover ordinary/reviewed sync, conflict/history
application through the transaction engine, and sync recovery. Other workflows
have command results and handler-level failures; supporting-file results also
record the returned transaction ID when available. Not every internal branch has
its own distinct code or stack trace. Components identify code areas, not a claim
of an exact failing source line. Nested file mappings may be unavailable when
planning fails before expansion or files/profile paths have changed.

Codes include `permission_denied`, `storage_full`, `not_found`, `already_exists`,
`timeout`, `canceled`, `conflict`, `observation_changed`, `lock_present`,
`pending_recovery`, and the conservative fallback `operation_failed`. Raw error
strings are never persisted to distinguish otherwise unclassified validation
errors. Use the stage, references and local read-only audit for further inspection.
The executable records unexpected mutating-command panics without the panic value
or raw stack and returns an error; pending state remains for inspection.

## Pending work and recovery

Doctor reports metadata presence for the manifest, sync lock, sync pending marker,
supporting-file pending marker and, when configured, coordinator lock and enrollment
pending marker. `present` does not validate a journal or establish that a lock is
stale. A malformed/unreadable profile still gets a sanitized `doctor` report, but
state discovery is limited. Bundle creation requires a valid profile so it can
reject output overlapping protected paths.

The service report includes registration/activity and known installation/apply
mode. `registered` on macOS does **not** prove the watcher is healthy. Unknown or
unavailable status is not a stopped/not-installed assertion. For detailed local
service metadata use `service status PROFILE`.

Do not delete a lock, pending marker, journal or receipt just to clear an error.
Confirm whether a writer is active, preserve independent edits, and inspect private
recovery data locally. Follow [sync recovery compatibility](upgrading.md#creation-ownership-recovery),
[supporting-file recovery](supporting-file-changes.md), or
[enrollment recovery](enrollment-creation.md), as appropriate. Diagnostics do not
authorize recovery or choose which conflicting version to keep.

## Sharing boundary

Bundles include sanitized status/build/platform information, counts, timestamps,
hashed references, transaction/run IDs and validated structured events. They
exclude native/config contents, literal identifiers/paths, arguments, environment
values, model conversations, stdout/stderr, native journal output, the local
reference map, and all backup/snapshot bodies. Records are decoded into the closed
schema and re-encoded; malformed, oversized, unknown-field or invalid-enum log
records are not copied verbatim. Unsupported custom build labels become `custom`.

Hashes are **not anonymization**: guessable paths/IDs can be correlated. Timestamps,
versions, counts and transaction IDs also reveal operational metadata. Review every
bundle before sharing. These protections are not a guarantee against malicious
software deliberately encoding secrets into otherwise valid metadata.

The bundle is staged privately, fsynced and exclusively linked into its requested
destination. A failed data write does not publish a partial final file; an existing
or racing destination is never replaced. Interrupted staging may leave a private
temporary file; inspect it before removal. No data is automatically transmitted.

Legacy macOS service `.out.log`/`.err.log` files remain private but unrotated and
can contain raw CLI diagnostics. Linux service stdout/stderr go to the user
journal under its own retention policy. `logs` gives their locations/query command,
but **neither stream is included in bundles**. Never assume they have the structured
logger's redaction or 4 MiB limit. See [service operations](services.md).

## Validation

The logging implementation milestone is `7828186`. Local validation on 2026-09-09
passed `go test -race ./...`, `go vet ./...`, and `go build ./cmd/agent-bridge`.
An additional isolated native-host + pinned-alpha-upgrade + native-service race
run passed. The real temporary launchd lifecycle separately passed (not skipped).
No real configuration/auth stores or production services were changed.

Regression tests cover retention/tail order, permissions, symlink/hardlink safety,
concurrent event writes and lock contention, secret canaries and forged records,
rate limiting, disk-full/short-write injection, no-partial/no-overwrite bundle
publication, managed-path protection, read-only commands, service reporting,
invalid profiles, pending markers, watch transitions, useful injected transaction
failure/recovery records, and process signal/restart correlation. Fixtures use
temporary homes. This is not a claim of 100% coverage or every OS/storage failure.

```sh
go test -race ./internal/diagnostics -count=1
go test -race ./internal/cli -run 'TestLogged|TestLogging|TestBundle|TestDoctor|TestConflictAndWatch' -count=1
go test -race ./internal/bridge -run '^TestObserved' -count=1
go test -race ./cmd/agent-bridge -count=1
```

The repository CI additionally runs the suite on macOS/Linux, exercises a real
disposable Linux user manager, and cross-builds macOS/Linux for amd64/arm64.
All seven jobs passed for the
[logging implementation milestone](https://github.com/Jhorlin/agent-bridge/actions/runs/34371242639).
Check the [workflow runs](https://github.com/Jhorlin/agent-bridge/actions/workflows/test.yml)
for each commit's outcome; local acceptance is not a claim that a pending CI run
has passed. Logs have their own schema: config 1, manifest 2 and recovery-journal 1
encodings remain unchanged.
