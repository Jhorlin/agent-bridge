# Claude Code ↔ Codex compatibility

Reviewed through 2026-09-09 source-build milestones. This is a feature-family audit of local configuration and extensibility, not an exhaustive inventory of every UI feature, flag, or enterprise policy. [Native acceptance tests](native-testing.md) cover specific discovery/validation operations in Claude Code 2.1.266 and Codex CLI 0.153.4, not complete interoperability or model behavior.

## Reading the matrix

- **Supported**: implemented and tested for the stated filesystem/configuration contract, not identical model behavior.
- **Partial**: a bounded subset is implemented; limitations are material.
- **Candidate**: not implemented; a possible mapping needs design and host verification.
- **Host-managed**: keep native configuration or runtime state separate for now; no equivalence claim.
- **Excluded**: deliberately outside automatic synchronization for safety.

“Candidate” is a design assessment, not a promise that every field can be translated. Source links describe host features; the implementation and tests determine bridge coverage. Unsupported structured adapter fields block conversion. The generic portable-file adapter does not inspect semantics and must not be used to bypass that boundary.

## Instructions and reusable workflows

| Feature | Native surfaces | Bridge today | Missing contract / next decision |
| --- | --- | --- | --- |
| Shared instructions | Claude `CLAUDE.md`; Codex `AGENTS.md` | Partial: explicit file sync or marker-delimited common sections preserving native overlays | Only manually reviewed common text; no tool-name or behavioral translation. [Portable adapters](portable-adapters.md) |
| Imports, scoped rules, precedence | Claude imports and `.claude/rules`; Codex hierarchical guidance | Candidate | Preserve host-only sections and scope; do not flatten conditional rules into unconditional instructions. |
| Skill contents | Both use `SKILL.md` and supporting resources | Partial: files, binary assets, executable bits, independent file edits | One explicit skill root; `portable: true` is human acknowledgment, not automated certification. [T2](#test-evidence) |
| Skill metadata and invocation | Claude frontmatter/invocation controls; Codex skill metadata and `agents/openai.yaml` | Partial: opt-in strict common name/description; exact body bytes | Models, tools, isolation, invocation policy and sidecars are rejected in strict mode. [Scope and tests](skill-metadata.md) |
| Custom commands | Claude conventional plugin commands; Codex migrated plugin skills | Partial in source builds | Flat compatibility-package commands with description and static body; host invocation names differ. Arguments/execution metadata rejected. [Limits](plugin-commands.md) |
| Custom subagent definitions | Claude agent Markdown/frontmatter; Codex agent TOML | Partial: name, description and instruction body; source builds can retain bounded host-local settings | Retention is opt-in, not model/permission translation or execution equivalence. [Portable adapters](portable-adapters.md) |
| Running agents / orchestration | Host-created workers and execution contexts | Host-managed | Definition translation would not transfer live workers, messages, task state, or model behavior. |

Native references: [Claude instruction loading](https://code.claude.com/docs/en/memory), [Claude skills](https://code.claude.com/docs/en/skills), [Claude agents](https://code.claude.com/docs/en/sub-agents), [Codex customization](https://learn.chatgpt.com/docs/customization/overview), [Codex skills](https://learn.chatgpt.com/docs/build-skills), [Codex agents](https://learn.chatgpt.com/docs/agent-configuration/subagents).

## MCP and plugins

| Feature | Bridge today | Missing contract / next decision |
| --- | --- | --- |
| MCP stdio / HTTP definitions | Partial: allowlisted servers, supported command/arguments, absolute cwd, HTTP URL; JSON ↔ TOML | Source builds merge independent server edits with recorded baselines; alpha remains whole-set. No SSE or remote executor mapping. [T3](#test-evidence) |
| MCP environment / header references | Partial: same-name environment forwarding and supported bearer/header references | No expansion by bridge, fallback/remapping, or general secret scanning. Credentials in arbitrary arguments can still be copied. [T3](#test-evidence) |
| MCP policy, enabled flags, timeouts | Partial in source builds: opt-in retention of bounded Codex-local fields | Policies never transfer to Claude; unknown fields still fail. [Policy retention](adapters.md#retaining-codex-local-mcp-policies-source-builds) |
| MCP runtime connectivity | Host-managed; local stdio fixture verified | Native tests verify Claude connection and Codex discovery, resource reads and direct tool calls. The bridge itself does not launch servers or authenticate; remote/authenticated transports remain unverified. |
| MCP formatting | Partial: semantic comparison, unrelated value preservation and verified scalar text patches | Source builds preserve surrounding bytes for supported scalar edits; structural/container edits still reformat. Explicit `allowReformat` remains required. [Limits](mcp-formatting.md), [T3](#test-evidence) |
| Plugin identity + portable skills | Partial: common metadata, portable skill assets, compatibility/portable Codex layouts | Authoring directories only; not a general plugin converter. [T4](#test-evidence) |
| Bundled MCP | Partial in source builds: explicit allowlist and conventional compatibility-package JSON | Native loading tested; root-relative execution remains blocked after failed Codex probes. [Contract](adapters.md#plugin-packages) |
| Hooks | Partial: startup SessionStart, plus prompt/Stop and exact-Bash pre/post events in source builds; bundled hooks require compatibility layout | Explicit timeout/absolute executable; bounded native denial, failure, timeout and trust tests, not arbitrary policy equivalence. [Tool hooks](tool-hooks.md) |
| Bundled agents, UI/app mappings and other components | Host-managed pending component-specific review | No silent dropping of components; unknown fields/layouts fail. [T4](#test-evidence) |
| Marketplace install / update / enable / trust / cache | Host-managed; fixture lifecycle verified | Native tests install/remove translated plugins in disposable hosts and verify Codex reinstall refresh. Bridge sync has no installation/refresh side effects; installed copies can remain stale. [Native evidence](native-testing.md) |

Native references: [Codex MCP](https://learn.chatgpt.com/docs/extend/mcp?surface=cli), [Claude extension overview](https://code.claude.com/docs/en/features-overview), [Claude plugin reference](https://code.claude.com/docs/en/plugins-reference), [OpenAI package formats](https://developers.openai.com/plugins/build/plugins). Exact implemented mappings are in the [adapter reference](adapters.md).

## Scope, synchronization, and safety

| Feature | Bridge today | Limits / intended boundary |
| --- | --- | --- |
| Global + project resources | Partial: explicit paths, inheritance, discovery, profile drafts and journaled creation/enrollment | Candidate consent stays explicit; no emulation of host precedence. [Enrollment](enrollment-creation.md) |
| Existing native root symlinks | Partial: explicit physical-target pins | No link creation, nested/chained links, hardlinks, aliases, or retargeting. [T5](#test-evidence) |
| Ongoing bidirectional sync | Supported for registered resources: one-second polling; writes opt in; macOS management and experimental Linux lifecycle | Real isolated Linux user-manager lifecycle passed; reboot/login and automatic upgrade remain unverified. [Services](services.md) |
| Read-only compatibility audit | Partial: per-resource planner checks, selected native field inventory, private diagnostics | Reports review requirements, not behavioral equivalence; no host execution or output compilation. [T8](#test-evidence) |
| Conflict / drift handling | Baselines and conflict blocking; source builds add reviewed side choices and historical file-version selection | No last-writer-wins; deliberate deletion and renames still need separate design. [Drift workflow](conflict-resolution.md) |
| Interrupted writes / recovery | Supported: private journals and guarded rollback | Per-file atomic replacement, not globally atomic visibility or proven power-loss durability. Separate state directories coordinate only when using the same explicit `coordinationDir`. [Coordination](onboarding.md#multiple-profiles) |
| Permissions / sandbox / enterprise policy | Host-managed | No translation; never infer equivalent security guarantees from similar setting names. |
| Model selection, reasoning, UI settings, shortcuts | Host-managed; bounded agent settings can be retained locally in source builds | No model equivalence mapping or cross-host UI settings synchronization. |
| Memory, conversations, resume state, scheduled tasks | Host-managed | No adapter or transfer contract. Any future handoff should be explicit, user-reviewed content, not automatic copying of internal state. |
| Cloud execution, IDE/UI integrations, billing, account entitlements | Host-managed | Outside this local configuration bridge; feature availability is not synchronized. |
| Login/session/OAuth credentials | Excluded | Do not transport auth stores; authenticate separately in each host. Environment references are not credential migration. |
| Arbitrary secret-bearing files | Excluded by intended use, not a comprehensive scanner | Users must review registered files; private whole-file journal snapshots may include unrelated sensitive values. |

## Test evidence

These are representative existing tests, not newly added coverage. All run against fixtures. A test named “EndToEnd” below covers the bridge CLI and filesystem, not either AI host.

| Evidence | Source and representative tests | What remains unverified |
| --- | --- | --- |
| T1 | [Engine tests](../internal/bridge/bridge_test.go): `TestEditsFromAllPeers`, `TestConcurrentConflict`, `TestDeletionConflict`, `TestIdenticalConcurrentEdits` | Instruction loading/meaning in each host |
| T2 | [Engine tests](../internal/bridge/bridge_test.go): `TestSkillBytesAndExecutables`, `TestSkillIndependentEditsAndAdditions`, `TestSkillPortabilityRequired`; [strict metadata tests](../internal/bridge/skill_test.go) and [native loading evidence](skill-metadata.md#evidence) | Host-specific metadata/invocation translation and real-model skill behavior |
| T3 | [Adapter tests](../internal/bridge/features_test.go): `TestMCPStdioRoundTrip`, `TestMCPHTTPBearerAndHeaderRefs`, `TestMCPCodexPolicyIsNotDropped`, `TestMCPSemanticFormattingIsNotDrift`, `TestMCPRollbackRestoresExactOriginalFormatting` | Host parsing, server startup, tools and authorization |
| T4 | [Adapter tests](../internal/bridge/features_test.go): `TestPluginPackageTranslationAndReverseEdit`, `TestPluginPortableCodexLayout`, `TestPluginUnsupportedComponentsBlockAllWrites`, `TestPluginFailureRecoveryAndNoInstallSideEffects` | Host installation, refresh, discovery and execution |
| T5 | [Adapter tests](../internal/bridge/features_test.go): `TestInheritanceUnchangedGlobalPathsAndDisable`, `TestPinnedFileSymlinkPreservedBidirectionally`, `TestPinnedRetargetDuringTransactionRollsBackOriginal` | Actual host scope resolution and concurrent profiles |
| T6 | [CLI tests](../internal/cli/cli_test.go): `TestWatchApplyAndShutdown`, `TestWatchWithoutApplyIsReadOnly`, `TestMCPWatcherEndToEnd` | Long-running service lifecycle and host reload behavior |
| T7 | [Engine tests](../internal/bridge/bridge_test.go): `TestPartialFailureRollback`, `TestProcessInterruptionRecovery`, `TestLaterEditBlocksRecovery`, `TestRecoveryTargetValidation` | Power loss, adversarial races, multi-profile locking |
| T8 | [Audit tests](../internal/bridge/audit_test.go): `TestAuditAllAdaptersReadOnlyAndDeterministic`, `TestAuditProfileInheritanceAndStrictFields`, `TestAuditUnsupportedAndMalformedMCPRedacted`; [CLI tests](../internal/cli/cli_test.go): `TestAuditFormatsExitCodesAndPrivacy`, `TestAuditOutputFailureIsRedacted` | Skill semantics, exhaustive nested-field inventory and live host acceptance |

## Implementation order and acceptance gates

The audit and bounded instruction/agent/startup-hook adapters are implemented. The [native harness](native-testing.md) verifies selected host operations. The remaining acceptance gates below are still required before expanding their support claims.

1. **Read-only compatibility audit — foundation implemented.** Go `audit CONFIG [--json]` reports resource ID, adapter, direction, recognized/known unsupported fields, redacted unknown-key counts and host-local actions. All seven adapters reuse planner checks; tests cover deterministic output, no filesystem writes, conflicts, pending state, unsafe links, malformed MCP, unsupported plugin components, profile rejection and diagnostic privacy. Native field inventory currently covers selected MCP server keys and plugin manifest top-level keys; nested/component failures can remain generic. The audit itself does not execute hosts, parse skill behavior or compile outputs. See [audit details](adapters.md#compatibility-audit). Scope is registered resources; separate discovery inventories explicit roots without enrollment.
2. **Instruction overlays and strict common skill metadata — implemented.** Marker-delimited common sections preserve native prefixes/suffixes and support reverse edits and conflicts. Opt-in strict skill mode validates and semantically reconciles name/description while preserving body bytes; native fixtures verify metadata loading. Host-specific metadata and invocation policy remain unsupported. See [strict skill mode](skill-metadata.md).
3. **Pinned-version host integration harness.** Use disposable homes/projects and exact Claude/Codex versions; verify supported isolation flags before launch. Record OS, versions, fixture hashes and outcomes. Begin with host discovery of a harmless skill and a local dummy MCP tool. No production credentials or live home access; any authenticated/model-backed run requires separate opt-in and cost awareness. Missing host binaries/access are reported as skipped, not passed.
4. **Broader MCP and bundled MCP.** Preserve host-local policies and formatting, then add plugin root/path handling and per-server reconciliation. Acceptance: unrelated edits/comments survive, independent server edits merge, restrictive settings cannot be weakened, and each claimed host-version pair loads the generated configuration.
5. **Minimal custom-agent mapping — implemented with native loading evidence.** Name, description and instruction body translate; models, tools and permissions remain unsupported and fail visibly. Local fake-provider tests verify Claude loads the agent instructions and Codex advertises the agent description. No claim of equivalent real-model execution or agent orchestration.
6. **Startup hooks and plugin lifecycle — bounded native tests implemented.** Local fake-provider tests verify startup event/source/cwd payloads in both CLIs and Codex's untrusted-hook skip behavior. Disposable plugin install/remove and Codex reinstall-refresh tests pass. Arbitrary hook payloads, timing, output and failure equivalence remain unverified. Native install/enable/trust controls are never synchronized or silently granted.
7. **Global onboarding and process lifecycle — partial.** Read-only discovery, empty-profile creation, optional common coordination, stable-input debounce, read-error retries, process-level SIGINT/SIGTERM/restart tests, and macOS service installation/start/stop/status/uninstall are implemented. Service removal preserves native files, state and backups. Linux service management and automatic resource ownership coordination remain future work. See [service scope and tests](services.md).

Release gate for any adapter: forward/reverse fixtures, idempotence, concurrent-edit and deletion tests, malformed/unknown-field rejection, privacy checks, exact rollback, and versioned host integration evidence. Configuration round-trips and host acceptance are separate checkboxes; neither certifies identical model decisions.

## Keeping this assessment current

On each adapter change or host-version upgrade, recheck the linked official documentation, record the date and tested versions, update this matrix and its tests together, and rerun affected host fixtures. New host features start as unassessed; they must not become supported just because a generic file copy succeeds. This document does not create a scheduled monitor or automatic upgrade process.
