# Native host acceptance tests

Run `AGENT_BRIDGE_NATIVE_TESTS=1 go test -v ./internal/bridge -run '^TestNative' -count=1` with both CLIs installed. Normal `go test` runs skip native acceptance tests. Missing binaries also skip, never pass as certified. Each native host process has a 20-second timeout and a minimal environment pointing its actual configuration roots at disposable fixture directories; tokens and auth helper variables are not inherited. The harness does not copy credentials, issue prompts, install plugins, or invoke model APIs. The MCP runtime test approves only its fixed Go fixture server in disposable Claude settings; it never approves a production server.

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

Codex plugin installation and agent execution, hook execution, host instruction
loading, and model behavior still need separate coverage. Machine-managed policies
may influence native CLIs even with disposable user directories; this is
configuration isolation, not an OS security sandbox.

Isolation references: [Codex environment variables](https://learn.chatgpt.com/docs/config-file/environment-variables), [Claude configuration locations](https://code.claude.com/docs/en/settings). Commands and flags are also checked against each installed CLI's help.
