# Interoperability status — 2026-09-10

This describes source builds, not a new published release. The bridge does not
provide complete one-to-one Claude Code/Codex feature parity. Configuration
convergence, native discovery, execution and authenticated service behavior are
different acceptance levels; passing one does not establish the others.

## Completed in this round

- Claude-local command model, tool-grant and argument-hint retention, with no
  permission/model export into Codex. Portable command bodies remain static.
  Pre-upgrade journal restoration retains current native settings.
- Quoted package-relative compatibility-hook executables, with bundled
  supporting files. Dependency checks cover merged edits, history, conflict
  choices, supporting-file deletion, rename and rename undo. No native trust
  is granted by synchronization.
- Bounded non-executable root documentation and image files. Unknown native
  configuration and competing host manifests remain blocked.
- Corrected nonempty MCP `cwd` handling: Claude ignores that field, so the
  bridge now rejects it instead of generating a misleading equivalent.
- Nested/underscore command paths and literal dollar text, with native migration
  collision detection. YAML-list argument hints and explicit false invocation
  flags stay Claude-local. Dynamic arguments and shell preprocessing still fail.
- Sh/bash package scripts and package-file arguments; missing dependencies are
  checked across sync, conflict choices, history and supporting-file changes.
- Source-specific omitted hook timeouts become explicit on the other host;
  explicit values support 1–600 seconds. Native defaults differ for prompt hooks.
- Empty non-executable `skills/.gitkeep` placeholders and ordinary spaces or
  parentheses in bounded root image filenames no longer block a package.
- Read-only native plugin candidate comparison without requiring a plannable
  synchronization profile. Multiple native manifests remain separate evidence;
  this command neither creates duplicates nor claims native equivalence.
- Opt-in global `protectNativePlugins` checks cached manifest names before
  automatic plugin adoption, history/conflict selection and writes. Existing
  native packages are never modified; inactive caches can also block adoption.
- Automatic skill adoption rejects reserved expansion-token prefixes and
  executable Markdown fences from either host. Literal dollar examples remain
  supported; explicitly reviewed portable skills retain their existing behavior.

Independent review found and regression tests closed pre-upgrade history,
prospective hook dependency, reserved filename-case and supporting-file mutation
gaps before this milestone was handed off.

## Evidence and limits

Native fixtures ran against Claude Code 2.1.267 and Codex CLI 0.153.4. All host
homes and configurations were disposable. Invocation fixtures used local fake
providers and inert scripts, not real model outcomes or service credentials.

For source milestone `bebf582`, the full Go race suite, vet, build, published-alpha
upgrade/recovery fixtures, opt-in native-host suite and isolated macOS service
test passed. Independent local review found no further actionable defects.
The separate ordinary coverage run measured **85.9% overall** (bridge package
87.0%); optional native tests are not included in that percentage. Coverage is
statement execution, not proof of feature parity or freedom from bugs.

| Check | Evidence |
| --- | --- |
| Command settings | Both hosts discover/invoke synchronized static text; local settings excluded from Codex output |
| Hook executables | Forward/reverse scripts execute from native-resolved roots containing spaces; Codex skips the untrusted hook |
| Root supporting files | Native install/remove and Codex cache comparison across compatibility and portable skill packages |
| MCP cwd mismatch | Claude connects an inert server but starts it in the session directory despite a different declared cwd |
| MCP root environment | An absolute inert script confirms Codex compatibility exports neither root variable; portable Codex exports `PLUGIN_ROOT` but changes cwd to the package. Claude compatibility exports `CLAUDE_PLUGIN_ROOT` with session cwd |
| CLI refresh | Claude requires a version bump for cached content; Codex re-add refreshes content but re-enables disabled plugins |
| Command path/expansion probes | Nested and underscore names invoke; literal dollars survive; native dynamic argument omission and normalized-name collisions reproduced |
| Interpreted hooks | Both hosts execute sh/bash, preserve session cwd, resolve roots with spaces and read package-file arguments; trust stays native |
| Hook defaults | Codex native metadata reports 600 seconds; Claude installed code/docs use 30 for prompt hooks and 600 for other supported events. Inert handlers execute with explicit 600; no ten-minute timeout experiment |
| Actual-data authoring survey | 10 of 33 copied packages converge across 63 managed items, up from 8 packages/56 items; 23 remain blocked |

The private survey inputs and provenance are not included in this public repo.
The aggregate result is **not** an execution certification for those packages.
Adapters stop at the first blocker, so a blocked package may have additional
incompatibilities after that one is addressed.

The new candidate inspector also ran against disposable copies of the native
Figma, Qodo and Stripe packages. All three had matching names but different
versions and component inventories. This is evidence against assuming complete
equivalence, not an authentication test or authorization to replace either copy.

## Still unsupported or externally gated

- Ongoing native installed-cache refresh that preserves enablement and trust.
  Reinstallation is not a transparent sync operation. See
  [native CLI evidence](plugin-copy.md#native-cli-refresh-evidence).
- General package-relative MCP: compatibility Codex leaves root tokens literal
  and supplies neither root environment variable to a launcher;
  portable Codex changes the working directory and Claude ignores `cwd`.
  A launcher would need its own reviewed runtime contract.
- General command argument/shell preprocessing, arbitrary hook events and
  interpreters beyond sh/bash, custom package layouts, direct bundled Codex agents and
  arbitrary host-only metadata. Rejection is not translation.
- Figma, Stripe and Qodo authenticated workflows still require native user
  sign-in. Installed counterparts are not proof of successful authentication.
- OAuth/session credentials, model identifiers, permission grants, sandbox
  enforcement and app-only runtime features remain native-managed. They cannot
  be made equivalent by copying configuration files.

The [completion checklist](completion-checklist.md) remains open for these
implementation and external gates. Do not describe this status as “100% tested”
or a completed full-parity release.
