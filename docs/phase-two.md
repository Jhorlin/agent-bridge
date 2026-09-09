# Phase two: eight workstreams, evidence before support claims

The user-approved next phase expands beyond v0.1.0-alpha.1. Begin with licensed
public upstream configurations, then add reviewed personal samples only with
explicit source selection. Tests always run against disposable homes/configs.
Real samples reduce blind spots; they do not prove every environment or future
host version. Known incompatibilities must remain explicit, never silently dropped.

## Work and acceptance gates

| # | Workstream | Required acceptance evidence | Current phase-two status |
|---|---|---|---|
| 1 | Discovery and enrollment | New resources on either side; reviewed enrollment; naming collisions, exclusions, scoped roots, rollback and no implicit trust | Read-only candidate watch implemented; automated reviewed enrollment remains pending; optional manual roster enforcement below |
| 2 | Plugin install/refresh | Explicit opt-in; source-to-cache version/digest checks; failure-safe update; preserve native enable/auth/trust choices | Planned; existing isolated lifecycle tests are groundwork |
| 3 | Complete plugin components | Bundled MCP, agents, commands and hooks; package-root relocation; path traversal rejection; forward/reverse native loading | Upstream manifest and bundled-MCP seed fixtures added; component support pending |
| 4 | Richer skills/agents | Field-by-field metadata, argument/dependency and host-local choice handling; reject non-equivalent policies; native discovery/invocation evidence | Upstream sidecar rejection fixture added; richer mappings pending |
| 5 | Additional hook events | Per-event input/output contract; tool-name mapping, ordering, timeout, failure and trust behavior in both hosts | Planned; startup-only baseline remains unchanged |
| 6 | MCP merging | Per-server baselines; independent/concurrent edits; preserve policies and formatting; package/transport fixtures; exact recovery | Plugin-relative upstream fixture added; implementation pending |
| 7 | Drift resolution | Reviewed conflict decisions; renames/deletions/history selection; preview; stale-input refusal and exact rollback | Planned; never default to last-writer-wins |
| 8 | Operational hardening | Overlapping-profile ownership; races/crash injection; Linux service lifecycle in Linux; upgrade/restart tests | Explicit-profile preflight and opt-in coordinator roster enforcement implemented; automated enrollment and other hardening pending |

Implementation sequence: fixture/evidence foundation, ownership and reviewed
enrollment, MCP and component contracts, richer definitions/hooks, plugin refresh,
drift workflows, then broader platform/hardening acceptance. Safety tests accompany
each increment rather than waiting until the end. Push only verified milestones;
do not modify or republish the existing alpha assets.

## Fixture and evidence rules

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

The initial [fixture catalog](../internal/bridge/testdata/upstream/catalog.json)
covers three configuration samples from two licensed components, not all eight
workstreams. It currently proves manifest-only round-tripping and safe rejection
of plugin-relative MCP and host-specific skill sidecars. Personal data is untouched.

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
disagreements. Reviewed enrollment remains pending; opt-in enforcement is described below.

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

## Documentation anchors

[Codex hooks](https://learn.chatgpt.com/docs/hooks) describes event behavior and
hash-based trust review. [Codex plugin building](https://learn.chatgpt.com/docs/build-plugins)
and [Claude hook documentation](https://code.claude.com/docs/en/hooks-guide) are
starting points, not proof of equal runtime semantics. Read the relevant current
contract and verify the installed versions before implementing each mapping.
