# Bounded shell-tool hooks

Source builds accept `PreToolUse` and `PostToolUse` with the exact matcher
`^Bash$`, alongside the existing startup/prompt/Stop definitions. Handlers remain
synchronous commands with one clean absolute executable path and an explicit
integer timeout of 1–60 seconds. This works for standalone `hook-config` and
conventional compatibility-plugin `hooks/hooks.json`. Other tools, wildcard
matchers, shell expressions, async/prompt/agent handlers and extra fields fail.

Native tests against Claude 2.1.266 and Codex 0.153.4 use a fixed harmless shell
command and loopback fake model providers in disposable homes. They verify:

- Both pre/post events identify `Bash`, the command, event and fixture cwd.
- Successful execution reaches the post hook with the expected tool output.
- A pre-hook JSON `permissionDecision: "deny"` prevents the post event and
  delivers its denial reason to the provider.
- A pre-hook exit status 1 **does not block** execution in either tested host.
- A one-second timeout stops the fixture hook but tool execution continues.
- Codex skips untrusted pre/post hooks; the successful fixtures use an explicitly
  reviewed, invocation-only trust override, not a stored trust grant.

These are narrow runtime checks, not a general policy translator or a complete
security boundary. The bridge copies the reviewed definition; it does not execute
or certify arbitrary hook scripts. It does not rewrite tool output schemas,
provide equivalent permission models, guarantee multiple-handler ordering, or
make failed hooks fail closed. Post hooks cannot undo a completed tool's effects.

Review the [Codex hook contract](https://learn.chatgpt.com/docs/hooks) and
[Claude hook contract](https://code.claude.com/docs/en/hooks) for host-specific
output behavior before using an existing policy script. Shared `Bash` event names
do not mean every tool, response field or approval behavior is equivalent.
