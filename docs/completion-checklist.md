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
- [ ] Expand actual plugin layouts/components only where native loading can be
  verified; enumerate incompatible commands, agents and hooks separately.
- [ ] Re-evaluate the actual-data plugin survey after each relevant adapter
  change; retain source provenance privately and never publish private inputs.
- [ ] Reconcile documentation and produce a versioned compatibility report with
  completed mappings, native-version evidence and remaining host limitations.

## External or host-specific gates

- Figma, Stripe and Qodo require native user sign-in for authenticated validation.
- Exact model names, permission grants, security enforcement, OAuth tokens and
  application-only runtimes are not interchangeable between hosts.
- A native counterpart is preferred over creating a competing translated copy;
  matching names alone do not prove equivalence.

## Acceptance for each implementation

Write and observe a failing regression, implement the mapping, test rejection,
reverse changes, conflicts, recovery and repeat sync, then run the full race
suite, vet and build. Run relevant native fixtures without real credentials or
real-home changes. Obtain independent review and push verified milestones.
Keep the existing published alpha immutable. Do not equate statement coverage
with interoperability coverage or claim 100% while an item remains open.
