# Native host acceptance tests

Run `AGENT_BRIDGE_NATIVE_TESTS=1 go test -v ./internal/bridge -run '^TestNative' -count=1` with both CLIs installed. Normal `go test` runs skip native acceptance tests. Missing binaries also skip, never pass as certified. Each native host process has a 20-second timeout and a minimal environment pointing its actual configuration roots at disposable fixture directories; tokens and auth helper variables are not inherited. The harness does not copy credentials or make paid model requests. Fixed prompts in the startup tests use loopback fake providers, not real models. Approvals, one-invocation hook trust overrides and plugin installs cover only reviewed fixtures in disposable settings and caches, never production resources.

Verified locally on macOS, 2026-09-08:

- Claude Code `2.1.266`; Codex CLI `0.153.4`.
- Codex `mcp get --json` accepts the bridge-generated stdio configuration and reports the expected command.
- Claude `mcp get` recognizes the generated project server before and after a reverse edit and leaves it pending approval. This version hides command details until approval, so this check does **not** prove server startup or execution.
- Claude `plugin validate --json` accepts the portable Claude package and the skills directory generated for Codex. This is Claude validation, **not** Codex plugin installation/discovery certification.

The tests exposed a faulty initial assertion: Claude's pending-approval output does not display command values. The assertion now verifies only what the host actually reports. No production approval was granted to make a test pass.

Additional native checks use the isolated Codex app-server JSON-RPC interface without starting a conversation or sending a model request: `skills/list` discovers a generated global skill; `hooks/list` discovers the generated startup command with `trustStatus: untrusted`. The returned event identifier is `sessionStart`, not the configuration spelling `SessionStart`. Claude's directory validator also accepts the minimal generated agent definition.

## MCP runtime acceptance

`TestNativeMCPStartupAndResourceRead` translates a stdio definition for a tiny Go
test-binary server, then verifies Claude reports `Connected`. It verifies Codex
discovers `bridge_echo`, reads `bridge://fixture`, and directly calls the tool in
an ephemeral thread. Each operation returns a fixed string; the fixture never
reads user files or uses the network. No model turn is started. The test proves
this local stdio runtime path, not remote HTTP authentication or arbitrary servers.
The initial pending-approval test remains separate and still verifies that the
bridge itself does not grant approval.

Broader agent orchestration, arbitrary hook failure semantics, and real model behavior still
need separate coverage. Machine-managed policies
may influence native CLIs even with disposable user directories; this is
configuration isolation, not an OS security sandbox.

## Disposable plugin lifecycle

The base plugin lifecycle tests install and remove a generated skill-only fixture.
Claude installs the reverse-translated package from a temporary local marketplace
and reports its updated version. Codex installs both supported manifest layouts,
discovers the skill from its installed cache, and removes it. The Codex test also
changes the authoring skill, verifies the installed copy stays unchanged, then
uninstalls/reinstalls and verifies discovery of the updated description. Authoring
sources survive removal. These tests use disposable host settings and caches;
they do not install anything in the user's actual hosts.

This validates a native lifecycle path, not automatic bridge-managed installation,
cache refresh, arbitrary plugin execution or hook trust. Users still install and
refresh reviewed packages with their native host tools.

## Expanded source-build checks (2026-09-09)

The same disposable harness additionally checks bounded plugin MCP, conventional
hooks and commands, [standalone agent exports](plugin-agent-exports.md), and the
[skill invocation-policy matrix](skill-invocation.md). Agent export tests verify
Codex discovery and Claude loading after reverse sync/fixture installation,
including retained local settings. Imported upstream agent instructions remain
inert; synthetic fixed-provider fixtures supply native loading evidence.

[Prompt/Stop and exact-Bash hooks](tool-hooks.md) have native input, denial,
error, timeout and trust checks. These do not establish equivalent arbitrary
output policies or scripts. The negative bundled-agent and plugin-root MCP probes
remain explicit unsupported boundaries, not successful feature acceptance.

[Published-alpha upgrade tests](upgrading.md) use a separate
`AGENT_BRIDGE_UPGRADE_TESTS=1` opt-in and build the pinned historical source.
[Linux service CI](services.md#linux-systemd-user-services-experimental-source-builds)
uses a disposable real user manager; it is separate from native AI-host testing.
No test result implies universal versions, reboot/login or automatic upgrades.

## Local fake-provider startup tests

Go HTTP test servers bound to loopback return fixed protocol responses. Codex uses
a custom Responses provider with no authentication requirement. Claude uses a
local Messages endpoint and a synthetic, nonfunctional test key. Neither endpoint
contains a model or forwards requests upstream. This exercises real CLI loading
and startup paths without API charges or subscription usage.

- Codex: a complete CLI turn leaves the capture script unexecuted while its hook
  is untrusted. A separate invocation with `--dangerously-bypass-hook-trust` runs
  only that reviewed fixture script. Its captured JSON has `SessionStart`,
  `startup`, and the expected disposable working directory. Both outgoing local
  requests contain the translated global shared instructions, skill description,
  and custom agent description (with multi-agent support enabled in the fixture).
- Claude: `--agent reviewer` loads a reverse-translated minimal agent. The local
  request contains its instruction body, the shared global instructions, and a
  strict-mode skill description generated from the Codex peer. The
  reverse-translated startup hook captures the expected event/source/directory.

The first app-server-only probe did not execute the startup hook; CLI startup is
the verified path. The mock Messages stream initially lacked event names; fixing
the fixture's SSE framing made the protocol test pass. These findings are not
bridge translation failures and were not counted as successful tests.

Tests do not prove real-model compliance, arbitrary script portability, timeout
or failure equivalence, or full subagent orchestration. The hook adapter never
adds a trust override to user configuration.

Protocol/configuration references: [Codex custom providers](https://learn.chatgpt.com/docs/config-file/config-advanced),
[Codex hook trust](https://learn.chatgpt.com/docs/hooks),
[Claude streaming events](https://platform.claude.com/docs/en/build-with-claude/streaming).

## Watcher process lifecycle

`TestNativeClaudeAlternateInstructionSources` verifies that Claude loads both
same-directory instruction sources, root first. `TestNativeComposedInstructionSources`
then synchronizes a set, edits its Codex alternate section, synchronizes back,
and checks both real hosts' loopback requests for both current instruction bodies
in order. Claude Code 2.1.267 and Codex 0.153.4 passed these checks. This proves
loading with fixed providers, not real-model compliance or unlimited context size.

The normal Go suite also exercises the executable entry point in child processes
on macOS/Linux. It synchronizes temporary files, sends SIGINT or SIGTERM, verifies
clean exit with no state/coordinator locks or pending journal, restarts, and
verifies another edit synchronizes. These tests do not install an OS background
service or certify power-loss recovery.

The separately opt-in [macOS service fixture](services.md#validation) does test a
real launchd job with temporary profiles and homes, including sync, stop, restart,
uninstall and persistent-data preservation. It does not test actual login/reboot.

Isolation references: [Codex environment variables](https://learn.chatgpt.com/docs/config-file/environment-variables), [Claude configuration locations](https://code.claude.com/docs/en/settings). Commands and flags are also checked against each installed CLI's help.

## Path-only file-guard acceptance

The opt-in `TestNativeCodexFileGuard` test forces a harmless native `apply_patch`
call through a loopback Responses fixture. With Codex 0.153.4, the permitted
patch creates its temporary file; the denied patch creates nothing and the next
provider request contains the policy denial. The project hook is produced by
`file-guard-config` from a Claude definition, not handwritten on the Codex side.
It uses only synthetic policy code,
disposable homes, and invocation-only trust of that fixture. It does not test or
authorize production hooks. Run with:

```sh
AGENT_BRIDGE_NATIVE_TESTS=1 go test ./internal/bridge -run '^TestNativeCodexFileGuard$' -count=1 -v
```

`TestNativeSkillLocalSettings` similarly proves explicit skill loading in Claude
Code 2.1.267 and Codex 0.153.4 while Claude-only metadata stays out of the Codex
file. These are native-host tests with fake providers, not paid model evaluations
or evidence of equivalent tool permissions. See [file-guard limits](file-guards.md)
and [native-local skill settings](skill-settings.md).
