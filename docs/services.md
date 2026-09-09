# macOS background watcher

The published alpha's service management is opt-in and macOS-only. It installs a per-user LaunchAgent
for one explicit profile; it does not discover resources, install Claude/Codex,
copy authentication, or grant native host trust. Linux users can run `watch`
under their own supervisor. Source builds additionally include the experimental
Linux implementation described below; its native lifecycle acceptance is pending.

Build the Go executable at a stable absolute location before installing. Do not
use `go run` or move/delete the binary while a service is installed. Review the
profile's resolved paths and audit first:

```sh
go build -o agent-bridge ./cmd/agent-bridge
./agent-bridge config /absolute/path/bridge.json
./agent-bridge audit /absolute/path/bridge.json
./agent-bridge service install /absolute/path/bridge.json
./agent-bridge service start /absolute/path/bridge.json
./agent-bridge service status /absolute/path/bridge.json
./agent-bridge service stop /absolute/path/bridge.json
./agent-bridge service uninstall /absolute/path/bridge.json
```

Installation defaults to **read-only preview**. To enable writes, uninstall the
preview service, then run `service install PROFILE --apply` and `service start
PROFILE`. Applying installation refuses current conflicts. The watcher retains
its normal debounce, conflict blocking, journal, and explicit-recovery rules.
Changes after installation are checked by the watcher, not implicitly approved.

## Lifecycle and ownership

- `install` creates a plist for the next login, an ownership receipt, and private
  log files. It does not immediately register a job. Existing plist/receipt files
  are never overwritten; partial installations require inspection.
- `start` registers the job in the current user's GUI launchd domain, or requests
  a start if already registered. A successful launchctl call does not prove the
  watcher stayed alive or that a sync succeeded: inspect status and logs.
- `status` is read-only JSON. `registered` means launchd has a job definition,
  **not** a health check. No installation returns `not-installed` without creating
  directories. Unknown native status is an error, not an assumed stopped job.
- `stop` unregisters the job for this login session, waiting up to 20 seconds per
  native operation. The installed plist remains and starts it at a later login.
- `uninstall` stops the job, verifies the exact owned plist and receipt again,
  and removes only those two files. It preserves all native/shared files, profiles,
  sync state, backups, and logs. It does not undo prior synchronizations. Edited,
  missing, or unrecognized ownership files fail closed; inspect them manually.

The label is `com.jhorlin.agent-bridge.<profile-path-hash>`. Files live under
`~/Library/LaunchAgents/`, with receipts and stdout/stderr logs in its
`.agent-bridge/` subdirectory. Logs are private but **not rotated**. Native host
settings and credentials are not embedded in the plist. The service runs as the
logged-in user, not root, and is not a system-wide daemon or an OS sandbox.

The binary receives the absolute profile path with no shell evaluation and runs
from the profile's directory. Relative profile resource paths keep their normal
declaring-file semantics. Profiles referring to service files/state are rejected
at installation. The ownership receipt assumes a trusted local user; it is not a
cryptographic authorization boundary against someone editing both receipt and plist.

There is deliberately no crash restart loop: transaction failures require human
inspection before `recover` or `start`. Login can start the installed watcher
again; pending journals still block synchronization. `stop` is not a persistent
disable command—use `uninstall` to prevent future login starts. Multiple profiles
sharing resources should explicitly use the same `coordinationDir`; automatic
resource ownership coordination is not provided.

## Validation

`go test -race ./...` covers lifecycle, ownership checks, private/unsafe logs,
unknown status and failed-stop preservation with a mock launchctl runner. The
opt-in native test builds a real binary and registers a unique temporary fixture
job in the current GUI domain, then exercises sync, stop, restart and uninstall:

```sh
AGENT_BRIDGE_SERVICE_TESTS=1 go test -race ./internal/bridge -run TestNativeLaunchService -v
```

It uses temporary homes/profiles only, requires macOS with an accessible GUI
domain, makes no model calls, and removes its exact fixture registration on exit.
A missing GUI domain is a skip, not a pass. This does not test reboot/login,
power-loss durability, every macOS version, or adversarial filesystem races.

Local acceptance on 2026-09-08: the real launchd fixture passed on macOS 26.4.1,
including first sync, stop, restart, second sync, removal and data preservation.

The plist lifecycle follows Apple's [launchd job documentation](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html);
native command behavior is verified against the installed `launchctl`.

## Linux systemd user services (experimental source builds)

The same `service install|start|stop|status|uninstall PROFILE` commands select the
Linux backend on Linux. Install is preview-only unless passed `--apply`. It creates
one private unit, a private content receipt, and an exact symlink under
`default.target.wants`, but never starts the user manager or watcher. The root is
`$XDG_CONFIG_HOME/systemd/user`, defaulting to `$HOME/.config/systemd/user`.
Custom XDG roots must also be known to the already-running user manager. No sudo,
lingering, system-wide services, environment import or credentials are configured.

Start reloads the manager, verifies the loaded fragment and absence of drop-ins,
then starts the exact unit and checks its active state. Stop waits for an inactive
state but leaves login enablement. Uninstall stops first and removes only the
unchanged owned symlink, unit and receipt; native files, baselines, backups and
journal logs remain. Modified files/links, overrides, inaccessible managers,
unknown states or stale locks fail closed. Partial installation/removal evidence
is retained, not automatically repaired. A post-removal daemon-reload failure
can report failure after owned files were removed; inspect before retrying.

Each systemctl invocation is bounded to 20 seconds. Timeout does not prove the
service stopped: its unit grants infinite graceful stop time so in-flight writes
can finish. Inspect the manager and pending journals rather than killing/restarting
blindly. Upgrade by stopping and uninstalling the owned unit, updating the stable
binary, then reinstalling with reviewed apply consent. In-place automatic upgrade
and native user-manager lifecycle validation remain pending.

Linux Docker tests cover the state machine with an injected manager and the
native systemd parser. They do not prove login/start/stop against a running
user manager: the unprivileged Docker user-manager probe did not start. This
implementation is therefore not yet release-accepted for unattended use.
