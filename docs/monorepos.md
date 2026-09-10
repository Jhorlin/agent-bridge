# Complex monorepos: staged adoption

A passing filesystem synchronization test is not proof of cross-host execution
or security equivalence. Inventory the actual checkout before enabling its watcher.
Do not publish proprietary instructions, skill bodies, tool arguments or credentials
as fixtures in this public repository; reproduce structural patterns synthetically.

1. Inspect native files, existing symlinks, upstream-owned trees, local edits and
   ignore rules. Never remove pre-existing `AGENTS.md` just because of its name.
2. Audit categories independently using private profiles outside the checkout.
   Do not apply those diagnostic profiles or run native tools against live secrets.
3. Fix genuine bridge bugs with failing synthetic regression tests first. Do not
   strip tool permissions or weaken a hook matcher to make a failing audit pass.
4. Select the safe subset in one project profile. Keep unsupported native sources
   untouched and record every exclusion and its reason in a private evaluation
   report. Ordinary sync is all-or-nothing for the selected inventory.
5. Review a fresh plan, apply, confirm a second plan has no writes, then start
   `watch --apply`. A controlled reversible edit can verify ongoing propagation;
   restore the edit and confirm source hashes and Git status are unchanged.

## Patterns requiring care

- **Collection documentation:** regular `README.md`, `LICENSE`, and `LICENSE.md`
  beside skill/plugin directories are ignored. They are not native components.
  Unknown files and unsafe links still fail discovery.
- **Informational skill fields:** current strict/invocation modes retain `license`,
  `compatibility`, and string-to-string `metadata`. These fields do not install
  prerequisites or translate tool permissions. An explicit
  [`preserveSkillSettings`](skill-settings.md) opt-in retains a bounded set of
  Claude-local settings, including `allowed-tools`, without granting Codex those
  approvals. Arbitrary custom fields and execution context remain unsupported.
- **Skill body expansions:** automatic resources reject recognized Claude-only
  argument/variable/shell expansion syntax. Literal examples also require review.
- **Instruction code examples:** import detection skips closed code fences and
  matched single-line code spans, following Claude's documented import behavior.
  Real imports remain blocked; an unterminated fence is ambiguous and rejected.
- **Existing instruction aliases:** a `CLAUDE.md` symlink to an existing `AGENTS.md`
  already shares a physical source. Do not create a second bridge owner over it.
  Exclude the pair or use a separately reviewed explicit mapping where safe.
- **Alternate instruction sources:** root `CLAUDE.md` plus `.claude/CLAUDE.md`
  compose into a source-marked `AGENTS.md` through the reversible
  [instruction-set adapter](instruction-sets.md). Alternate-only directories are
  supported too. Lowercase variants remain blocked, including on case-insensitive
  filesystems; preserve and review those separately.
- **Scoped rules:** `.claude/rules` generates a warning, not translated rules.
  Claude path triggers and Codex instruction loading are not the same contract.
- **Hooks:** matching `Glob|Grep` or `Edit|Write|NotebookEdit` cannot be changed
  into `^Bash$` without changing policy. Leave unsupported hooks native-local.
  For a separately reviewed path-only script, the explicit
  [file-guard runtime adapter](file-guards.md) can inspect native patch targets.
  It is not automatic definition synchronization or complete filesystem mediation.
- **Upstream-owned instructions:** generated peers may complicate fork upgrades.
  Exclude upstream files and retain their existing aliases during a local trial.

Use local `.git/info/exclude` entries to keep trial profiles, state, reports and
generated peers out of a corporate repository without editing tracked ignore
policy. Bridge discovery does not read Git ignore rules: private/generated roots
still need explicit convention exclusions. A future unsupported component pauses
the watcher rather than being silently flattened or partially applied.

Discovery still walks eligible project directories for new components, but only
probes native feature collections at directories containing native configuration
entries. Unsafe links remain visible to validation, and previously managed
collections remain tracked when their files disappear. This reduces redundant
probes in large source trees without caching away new assets or deletions.

The regression suite covers these structural cases using invented content.
Targeted native tests verify discovery, not arbitrary scripts, live MCP connectivity,
permission parity, plugin installation, or correct behavior of proprietary skills.
