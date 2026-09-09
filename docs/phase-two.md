# Phase two: eight workstreams, evidence before support claims

The user-approved next phase expands beyond v0.1.0-alpha.1. Begin with licensed
public upstream configurations, then add reviewed personal samples only with
explicit source selection. Tests always run against disposable homes/configs.
Real samples reduce blind spots; they do not prove every environment or future
host version. Known incompatibilities must remain explicit, never silently dropped.

## Work and acceptance gates

| # | Workstream | Required acceptance evidence | Current phase-two status |
|---|---|---|---|
| 1 | Discovery and enrollment | New resources on either side; reviewed enrollment; naming collisions, exclusions, scoped roots, rollback and no implicit trust | Discovery, drafts, existing-profile enrollment and journaled creation/registration implemented; candidate consent remains explicit, not automatic |
| 2 | Plugin install/refresh | Explicit opt-in; source-to-cache version/digest checks; failure-safe update; preserve native enable/auth/trust choices | Read-only selected-copy comparison and isolated lifecycle tests implemented; production install/refresh awaits an appropriate supported native contract |
| 3 | Complete plugin components | Bundled MCP, agents, commands and hooks; package-root relocation; path traversal rejection; forward/reverse native loading | Bounded conventional hooks, static commands and allowlisted MCP plus explicit standalone agent exports implemented with native loading tests; in-package Codex agents and package-root relocation remain unsupported |
| 4 | Richer skills/agents | Field-by-field metadata, argument/dependency and host-local choice handling; reject non-equivalent policies; native discovery/invocation evidence | Bounded host-local agent settings and opt-in skill invocation-policy translation implemented with native loading/invocation tests; argument/dependency mapping remains pending |
| 5 | Additional hook events | Per-event input/output contract; tool-name mapping, ordering, timeout, failure and trust behavior in both hosts | Prompt/Stop plus exact-Bash pre/post definitions, native payloads, denial, failure, timeout and Codex trust checks implemented; other tools/events and arbitrary policy equivalence pending |
| 6 | MCP merging | Per-server baselines; independent/concurrent edits; preserve policies and formatting; package/transport fixtures; exact recovery | Independent server merging, rollback, opt-in Codex-local policy retention and verified scalar text patches implemented; structural formatting preservation and plugin-relative support remain pending |
| 7 | Drift resolution | Reviewed conflict decisions; renames/deletions/history selection; preview; stale-input refusal and exact rollback | Reviewed side/history choices, journaled supporting-file rename/delete and undo, plus [whole-resource retirement preserving files](retirement.md) implemented; native entry-point deletion and automatic retirement undo are not provided |
| 8 | Operational hardening | Overlapping-profile ownership; races/crash injection; Linux service lifecycle in Linux; upgrade/restart tests | Ownership checks, Linux lifecycle, user-manager restart and same-source replacement verified; pinned published-alpha data/journal upgrade tests added separately. Reboot/login, arbitrary-version migrations and automatic upgrades remain outside current acceptance |

Implementation sequence: fixture/evidence foundation, ownership and reviewed
enrollment, MCP and component contracts, richer definitions/hooks, plugin refresh,
drift workflows, then broader platform/hardening acceptance. Safety tests accompany
each increment rather than waiting until the end. Push only verified milestones;
do not modify or republish the existing alpha assets.

Bundled agents remain a real compatibility gap, not an untested copy operation:
an isolated Codex 0.153.4 probe did not advertise either `agents/*.md` or
`agents/*.toml` from an installed compatibility plugin, while a standalone
`CODEX_HOME/agents/*.toml` positive control was advertised in the same request.
Direct Codex package agents remain rejected. An opt-in
[standalone export](plugin-agent-exports.md) now provides a separately owned
alternative with positive native discovery evidence. Its independent lifecycle
is not equivalent to an installed plugin component.

## Fixture and evidence rules

Source-build drift commands and their exact limitations are documented in
[reviewed conflict and historical selection](conflict-resolution.md). Tests cover
all current adapter types for conflict selection, current instruction-overlay
preservation for history, stale choices/raw inputs/journal bytes, unsafe history,
ownership checks, exact rollback, repeat sync and CLI error handling. These are
model-free filesystem operations, not native host invocation or deletion support.

1. Record exact public repository revision, source file/blob, local digest,
   applicable license and any required attribution. Public visibility alone is
   not permission to redistribute or use a plugin across hosts.
2. Inspect the selected configuration before import. Never import auth stores,
   token helpers, private histories, enterprise secrets or an entire home tree.
3. Separate unchanged source data, sanitized derivatives and generated execution
   substitutes. Do not call a synthetic happy-path sample actual user data.
4. Every fixture gets an offline integrity test and a behavior assertion. Every
   desired mapping gets forward/reverse, repeat-sync, conflict/deletion, malformed
   input, privacy, rollback and relevant native tests. No imported script executes
   merely because it appears in a fixture.
5. Record states precisely: planned, implemented/model-free tested,
   native-version verified, known unsupported, or blocked awaiting evidence.
   An expected-rejection test is a safety check, not completed feature support.
6. Pin native host versions and report skips. Use local fake providers for protocol
   tests. Real authenticated calls need separately scoped consent and a cost cap.
7. Enumerate unresolved contracts. A release cannot claim broad support while a
   claimed field/event has no evidence; do not rename unverified work to “done.”

The [fixture catalog](../internal/bridge/testdata/upstream/catalog.json)
covers four samples from three licensed components, not all eight workstreams.
It verifies manifest round-tripping, explicit agent export with local settings,
and safe rejection of plugin-relative MCP and host-specific skill UI sidecars.
Personal data is untouched; imported instructions never execute.

## First discovery increment

`agent-bridge watch-discovery ROOT --global` (or `--project`) emits an initial
JSON discovery report, then another JSON line when the candidate list changes.
It polls once per second, suppresses unchanged inventories and exits cleanly on
Ctrl-C. It uses the same explicit conventional roots and exclusions as `discover`:
no installed plugin caches, credentials/history, arbitrary project traversal or
file-content reads. An unsafe/unavailable root or output error stops it with exit 1.

This watches candidate identities/paths, not skill contents or whether an already
known candidate now also exists on its other peer. It does not create profiles,
write state, enroll resources, execute hooks or grant portability/trust. There is
no `--apply` flag. Reviewed enrollment is the next step, not implicitly complete.

## Selected profile drafts

After `discover`, explicitly select candidate IDs to prepare a review envelope:

```sh
agent-bridge draft-profile /absolute/project --project instructions skill-directory-example
```

Use `--global` with an explicit home-shaped fixture/root for global candidates.
Output is JSON on stdout only: a `profile` proposal, `reviewRequired: true`, and
review guidance. It is deliberately not a runnable configuration. No file,
state, ownership roster, watcher, or native configuration is created or changed.
The inventory is refreshed; missing IDs, duplicate selections, case-ambiguous
candidates and unsafe links fail. Output/error failures return exit 1.

This reads names/metadata, not native contents. It does not pin content, verify
host behavior or grant portability/trust. Review both sides privately, choose
a non-overlapping state directory, then extract/edit `profile` into a new private
configuration. Skill/agent/hook consent flags remain false; MCP server selection
and reformat consent remain unset. Instructions default to a whole-file candidate:
choose `instruction-file` and prepare marked sections if sharing only part of it.
Run `audit`, `plan`, and `check-overlap` before enabling any writes. Never turn on
all consent flags just to make validation pass. For transactional profile creation
and registration with stale-content refusal and rollback, use the separate
[reviewed enrollment workflow](enrollment-creation.md).

## Guarding a reviewed sync

After manually preparing a new private configuration from the draft, run:

```sh
agent-bridge review-profile /absolute/path/profile.json
agent-bridge sync-reviewed /absolute/path/profile.json OBSERVATION_FROM_REVIEW
```

`review-profile` strictly validates the profile and plans without writing. A
conflicting/unsupported plan returns exit 1 without an observation. Successful
JSON includes an `observation` SHA-256 digest and summaries, not file contents.
Review the actual private content and the proposed writes before proceeding.

`sync-reviewed` is an explicit write command. It replans under the existing locks
and requires the digest to match the effective configuration, planned raw managed
inputs and baseline manifest. Stale input returns exit 2 before transactional
target writes; missing/invalid digests never fall back to unconditional sync.
Lock/state directories may still be created during an unsuccessful attempt.
Existing ownership checks, per-write comparisons, journals and rollback apply.
Other errors return exit 1; inspect pending recovery state before retrying. An
output failure after successful sync does not undo that sync.

The digest is a freshness check, not a secret, signature, proof of human approval,
or host trust grant. It does not pin comments/formatting in the profile, runtime
host behavior, omitted files or external changes after checking. Roster policy
is checked live, not included in the digest. Review output can expose resource
IDs and relative file names, so keep it private. These commands operate on an
existing reviewed profile. Separate commands support [journaled creation and
registration](enrollment-creation.md) and [historical file-version selection](conflict-resolution.md).
Automatic enrollment remains deliberately unsupported.

## Enrolling an existing reviewed profile

After preparing a new private profile with an explicit `coordinationDir`, run
`review-profile CONFIG`, inspect its contents/plan, then use
`enroll-reviewed CONFIG OBSERVATION`. This adds that existing profile to the
coordinator's roster under its shared lock. It rejects stale reviewed inputs,
overlaps, inconsistent coordinators, invalid rosters and unsafe paths. Repeating
an unchanged enrollment is a no-op. No native data or synchronization baseline
is written, no watcher is started, and no compatibility consent is inferred.

The roster is the only committed file. First creation uses an exclusive link of
a fully written temporary file; updates use atomic rename. A private before/after
snapshot is retained under `coordinationDir/enrollment-backups/UUID/roster.json`
before any change. A failed update may leave this backup. This is not a signed
approval receipt or power-loss durability guarantee. Stop participants before
external edits; uncooperative edits racing the final check are not fenced.
Output failure after commit does not undo enrollment. Inspect backups privately
before manual restoration; automatic historical rollback remains unfinished.

Profile creation remains an explicit review step: this command does not extract
draft envelopes, overwrite profiles, remove roster entries or perform a two-file
profile-plus-roster transaction. Exit 0 is success, 2 is stale review, 1 is an
operational/validation error. After enrollment, use a fresh review and
`sync-reviewed` when ready to apply the actual resource changes.

## Cross-profile overlap preflight

`agent-bridge check-overlap profile.json other.json [more.json ...]` strictly
loads the explicitly listed profiles and returns a read-only JSON report. Exit 0
means no path overlap found, 2 means overlaps, and 1 means invalid/unsafe input or
an output failure. It checks native paths (including pinned link paths/targets),
state directories, inherited configuration paths and coordination directories.
Equal shared configuration and coordination paths are allowed. Nested paths and
case-only collisions are conservatively reported, even on case-sensitive systems.

The report contains profile/resource paths, not native configuration contents.
No directory, registry, lock or baseline is written. This is a point-in-time
preflight, **not persistent ownership enforcement**: profiles omitted from the
command, later edits, filesystem aliases such as hard links, and concurrent
changes are not covered. Passing it does not grant portability or trust, and
does not replace `audit` and `plan`. Do not run overlapping profiles merely
because they share a coordinator: serialization alone does not prevent baseline
disagreements. Reviewed existing-profile enrollment and opt-in enforcement are described below.

## Opt-in ownership enforcement

To enforce the reviewed profile set, give every participating profile the same
`coordinationDir`. While all watchers/services are stopped, create a private
`profiles.json` in that directory, containing explicit absolute profile paths:

```json
{
  "version": 1,
  "profiles": ["/absolute/path/global.json", "/absolute/path/project.json"]
}
```

Run `check-overlap` on those profiles before restarting. With a roster present,
sync and recovery validate membership, the shared coordinator, current loaded
configuration and path overlaps **under the common lock**, before taking the
state lock or writing native files. Invalid, missing, symlinked or unknown-field
profiles/rosters block writes with redacted diagnostics. Conflicts preserve any
pending recovery journal; restore a valid, non-overlapping reviewed roster before
retrying recovery. Later native edits still require inspection as before.

This is cooperative enforcement for explicitly configured participants, not an
OS security boundary or automatic enrollment. A missing roster retains legacy
lock-only behavior. Removing it disables enforcement; do not use deletion to
bypass a conflict. Unlisted profiles using another/no coordinator, older binaries,
hard-link aliases and external edits during a running transaction are not fenced.
Stop all participants before editing the roster or profiles, keep the roster
private (0600), and preserve it with configuration backups. No roster or live
configuration is created automatically. Existing config/manifest/journal schemas
and the published alpha remain unchanged.

## Per-server MCP merging

Source builds record a hash for each selected server after a successful sync.
Subsequent edits to different servers can merge across shared, Claude and Codex
inputs. Identical edits to one server converge; differing edits to that same
server block every write. Native documents remain single transactional writes,
with unchanged rollback and later-edit checks. Unselected entries and unrelated
document fields are preserved semantically. Source builds also preserve surrounding
comments/layout for [supported scalar edits](mcp-formatting.md); structural edits
can still reformat. Unknown policies, literal credentials, plugin-relative interpolation,
partial allowlists and selected-server deletions remain unsupported.

Manifest schema remains 2: additional hash entries record granular baselines and
their corresponding whole-set digest. A legacy manifest or an older writer that
leaves stale granular hashes falls back to whole-set conflict detection. A
successful non-conflicting sync seeds current hashes; do not erase baselines to
force a merge. No automatic migration resolves an existing conflict. Initial
bootstrap still requires the selected sets to agree or exist on only one side.

Tests use synthetic, isolated three-server edit scenarios, including concurrent
edits on all three sides, same-server conflicts, deletion refusal, repeat sync,
legacy fallback and injected rollback. The public upstream corpus still verifies
rejection of unsupported plugin-relative MCP; no new transport/runtime behavior
or native execution support is claimed by this reconciliation change.

## Linux unit export

`agent-bridge systemd-unit ABSOLUTE_PROFILE ABSOLUTE_BINARY [--apply]` validates
an explicit profile and existing executable, then emits a systemd user unit to
stdout only. It never installs/enables/starts a service, changes a roster or
writes native configuration. Paths must be valid on the intended Linux host;
use canonical paths without symlink ancestors. Executable paths containing `$`
or `%` are rejected. Profile arguments are quoted with literal dollar/percent
escaping; no shell is used. Apply-mode export rejects current conflicts.

The generated watcher is preview-only unless `--apply` is selected. `Restart=no`
prevents error restart loops. `TimeoutStopSec=infinity` permits in-flight sync
to finish but means a stuck process needs manual inspection. Output goes to the
user journal: resource names/paths may appear, and journal access/retention is a
host policy, not private-file logging. `UMask=0077` governs newly created files.
No credentials, environment values or host trust settings are embedded.

Offline Linux validation uses `systemd-analyze verify` through the opt-in
`AGENT_BRIDGE_SYSTEMD_TESTS=1` test. Parser acceptance does **not** establish a
working login/start/stop/uninstall lifecycle. Automatic installation, receipts,
ownership-safe removal are implemented in the experimental Linux backend;
actual user-manager lifecycle acceptance subsequently passed in the isolated
[Linux CI fixture](services.md#linux-systemd-user-services-experimental-source-builds).
Automatic upgrades and reboot/login acceptance remain pending.
The macOS test fixture resolves Go's temporary executable path explicitly;
production still rejects symlink paths. Quoting follows the
[upstream systemd service specification](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).

## Documentation anchors

### Prompt and Stop hook evidence

The source adapter now maps `UserPromptSubmit` and `Stop` alongside startup-only
`SessionStart`. No matcher is accepted for the two new events; explicit command
timeout and executable restrictions remain. Fixture tests exercise forward/reverse
sync, repeat sync, conflict, rollback and rejected matcher handling. Installed
Claude Code 2.1.266 and Codex 0.153.4 execute isolated fixture commands; assertions
check event/cwd, prompt text, and the Stop recursion flag. Codex's untrusted skip
is tested separately from a one-invocation fixture-only trust override.

These model-free tests use local fake endpoints, not authenticated model calls.
They do not prove arbitrary hook scripts, blocking/continuation policies, timeout
semantics, context precedence, ordering, or retry limits are equivalent. Review
those behaviors explicitly: Claude and Codex document host-specific differences.
Tool-name mappings remain outside this increment. Conventional bundled
`hooks/hooks.json` uses the same bounded adapter with `.codex-plugin/plugin.json`.
Native tests verify reverse-generated Claude package installation and Codex
discovery as untrusted. Root-manifest portable Codex packages did not expose
their hooks in the installed host test; such packages now fail conversion.
Custom hook manifest paths and plugin-root command interpolation remain rejected.

[Codex hooks](https://learn.chatgpt.com/docs/hooks) describes event behavior and
hash-based trust review. [Codex plugin building](https://learn.chatgpt.com/docs/build-plugins)
and [Claude hook documentation](https://code.claude.com/docs/en/hooks-guide) are
starting points, not proof of equal runtime semantics. Read the relevant current
contract and verify the installed versions before implementing each mapping.
