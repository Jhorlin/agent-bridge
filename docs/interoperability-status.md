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

Independent review found and regression tests closed pre-upgrade history,
prospective hook dependency, reserved filename-case and supporting-file mutation
gaps before this milestone was handed off.

## Evidence and limits

Native fixtures ran against Claude Code 2.1.267 and Codex CLI 0.153.4. All host
homes and configurations were disposable. Invocation fixtures used local fake
providers and inert scripts, not real model outcomes or service credentials.

| Check | Evidence |
| --- | --- |
| Command settings | Both hosts discover/invoke synchronized static text; local settings excluded from Codex output |
| Hook executables | Forward/reverse scripts execute from native-resolved roots containing spaces; Codex skips the untrusted hook |
| Root supporting files | Native install/remove and Codex cache comparison across compatibility and portable skill packages |
| MCP cwd mismatch | Claude connects an inert server but starts it in the session directory despite a different declared cwd |
| CLI refresh | Claude requires a version bump for cached content; Codex re-add refreshes content but re-enables disabled plugins |
| Actual-data authoring survey | 8 of 33 copied packages converge across 56 managed items, up from 6 packages/36 items; 25 remain blocked |

The private survey inputs and provenance are not included in this public repo.
The aggregate result is **not** an execution certification for those packages.
Adapters stop at the first blocker, so a blocked package may have additional
incompatibilities after that one is addressed.

## Still unsupported or externally gated

- Ongoing native installed-cache refresh that preserves enablement and trust.
  Reinstallation is not a transparent sync operation. See
  [native CLI evidence](plugin-copy.md#native-cli-refresh-evidence).
- General package-relative MCP: compatibility Codex leaves root tokens literal;
  portable Codex changes the working directory and Claude ignores `cwd`.
  A launcher would need its own reviewed runtime contract.
- General command argument/shell preprocessing, arbitrary hook events and
  interpreters, nested/custom package layouts, direct bundled Codex agents and
  arbitrary host-only metadata. Rejection is not translation.
- Figma, Stripe and Qodo authenticated workflows still require native user
  sign-in. Installed counterparts are not proof of successful authentication.
- OAuth/session credentials, model identifiers, permission grants, sandbox
  enforcement and app-only runtime features remain native-managed. They cannot
  be made equivalent by copying configuration files.

The [completion checklist](completion-checklist.md) remains open for these
implementation and external gates. Do not describe this status as “100% tested”
or a completed full-parity release.
