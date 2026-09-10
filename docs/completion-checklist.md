# Remaining interoperability work

This tracks the completion request after `9f90d35`. Passing the current suite
does not complete these items. A rejected input is not supported functionality.

## Implementation queue

- [x] Preserve reviewed Claude-only command metadata without exporting model or
  tool grants; verify forward/reverse sync, history, conflicts and native loading.
- [ ] Establish a native CLI install/refresh contract that preserves existing
  enablement, authentication and trust. Test source/version changes and disabled
  plugins in disposable homes before adding production lifecycle commands.
- [ ] Support package-relative MCP and hook dependencies with explicit safe path
  translation and native runtime evidence, not absolute cache-path copying.
  Quoted compatibility-hook executables and sh/bash scripts with package-file
  arguments are implemented and tested, including argument dependency recovery.
  MCP remains
  blocked: compatibility Codex does not expand its package root, portable Codex
  changes cwd, and Claude ignores configured cwd.
- [ ] Expand actual plugin layouts/components only where native loading can be
  verified; enumerate incompatible commands, agents and hooks separately.
  Bounded root documentation/images, nested/underscore command paths, literal
  dollar text and native-local metadata shapes now pass native tests. Command
  name collisions fail closed. Custom layouts and conflicting host manifests
  remain unsupported; read-only native candidate comparison is implemented.
- [x] Re-evaluate the actual-data plugin survey after each relevant adapter
  change; retain source provenance privately and never publish private inputs.
  Latest copied-input run: 10/33 packages converge, 63 managed items; 23 blocked.
- [x] Reconcile documentation and produce a versioned compatibility report with
  completed mappings, native-version evidence and remaining host limitations.
  Current [dated source-build report](interoperability-status.md) is available;
  this does not close the remaining full-parity release requirements.
- [x] Add opt-in ongoing global cached-plugin name collision protection, with
  prospective rename/history/conflict and pre-journal rechecks. This blocks
  potential duplicates; it does not automatically select or install counterparts.

## External or host-specific gates

- Figma, Stripe and Qodo require native user sign-in for authenticated validation.
- Exact model names, permission grants, security enforcement, OAuth tokens and
  application-only runtimes are not interchangeable between hosts.
- Native Codex 0.153.4 refresh re-enables disabled plugins; Claude same-version
  refresh keeps old cached content. See the repeatable [CLI contract
  tests](plugin-copy.md#native-cli-refresh-evidence). Automatic cache refresh
  preserving those choices is not implemented or implied by authoring sync.
- A native counterpart is preferred over creating a competing translated copy;
  matching names alone do not prove equivalence.

## Acceptance for each implementation

Write and observe a failing regression, implement the mapping, test rejection,
reverse changes, conflicts, recovery and repeat sync, then run the full race
suite, vet and build. Run relevant native fixtures without real credentials or
real-home changes. Obtain independent review and push verified milestones.
Keep the existing published alpha immutable. Do not equate statement coverage
with interoperability coverage or claim 100% while an item remains open.
