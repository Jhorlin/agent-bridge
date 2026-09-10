# Adapting path-based edit guards

`agent-bridge hook-file-guard ABSOLUTE_PROJECT ABSOLUTE_EXECUTABLE` reads one
Codex `PreToolUse` event from stdin. For a supported `apply_patch` command, it
invokes the explicitly reviewed executable separately for every affected path,
including both ends of a move. Each invocation receives an absolute
`tool_input.file_path` and `tool_input.path`, `tool_name: Edit`, the original patch
in `tool_input.command`, and the event cwd. `CLAUDE_PROJECT_DIR` is set to the
explicit project. The executable must be a regular executable file, not shell
syntax. The bridge never applies the patch itself.

This adapter is for **path-only guards**, not arbitrary Claude Edit/Write scripts.
It does not synthesize `old_string`, `new_string`, notebook cells, or Write content.
It treats add/update/delete/move as edit-policy checks; a script that distinguishes
tool names needs separate review. It is not automatically selected by convention
sync, and it does not change the source Claude hook definition.

Example of a reviewed Codex-local hook (replace both absolute paths):

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "^apply_patch$",
      "hooks": [{
        "type": "command",
        "command": "/absolute/agent-bridge hook-file-guard /absolute/project /absolute/project/path-policy",
        "timeout": 15
      }]
    }]
  }
}
```

Review and trust it through the host's normal hook workflow. Do not bypass trust
for real projects. Native tests use invocation-only trust for reviewed synthetic
fixtures in disposable homes, a loopback provider, and no paid model requests.

## Safety and limits

- Unknown/malformed events, patch syntax, output, unsafe paths, outside-project
  targets, symlinks, parent-directory (`..`) components, duplicate JSON keys,
  execution errors and timeouts deny. Parent traversal is rejected before path
  cleaning so it cannot conceal a symlink.
- Only a strict patch grammar subset is supported. Environment overrides, shell
  heredoc wrappers, CRLF patches and ambiguous constructs require review.
- Input is limited to 1 MiB, targets to 256, stdout/stderr to 64 KiB each, and
  policy execution uses a shared ten-second deadline starting at validation.
  The handler timeout should be longer; blocked stdin or filesystem I/O still
  depends on the host timeout.
- An explicit deny is propagated. A script's allow does not become a Codex
  permission grant. Additional context is bounded; rewritten input is rejected.
- These failure semantics are deliberately stronger than native hook exit-1
  behavior. If the bridge executable itself cannot start or the host times out
  first, the host's failure behavior still applies.
- Child process groups are reaped. This command executes trusted policy code with
  the caller's environment; it is not a sandbox for untrusted scripts.
- Shell writes and other tools are outside this matcher. File replacement races,
  hardlinks, or an administrator disabling hooks are not comprehensively mediated.
  Use the native filesystem sandbox for enforcement, not hooks alone.
- The executable is explicit: changes to which script a Claude hook references
  require reviewing/updating this mapping. Do not assume automatic command drift
  translation. Existing project hook settings must be merged deliberately, never
  overwritten as a convenience.
- Denials are returned to the host in the hook result, so inspect its hook trace
  when troubleshooting. This standalone helper has no profile and does not append
  to the bridge's profile sync logs. Script stderr is bounded but not forwarded;
  reproduce with a harmless private payload to inspect a reviewed script itself.

Regression tests cover multi-file patches, moves, malformed syntax, unsafe paths,
duplicate/unsupported output (including case aliases), bounded execution and no
approval escalation. Native
Codex tests demonstrate a permitted fixture patch and a denied patch that creates
no file and delivers its denial to the loopback provider.

References: [Codex hook payloads](https://learn.chatgpt.com/docs/hooks#pretooluse)
and [native patch grammar](https://github.com/openai/codex/blob/main/codex-rs/apply-patch/src/parser.rs).
