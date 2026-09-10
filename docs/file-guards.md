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
sync. The helper alone does not change the source Claude hook definition. Source
builds also support the explicit definition adapter described below.

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
- Any script stderr also denies, even with exit status zero or an explicit allow
  result. Shell scripts can accidentally swallow missing-dependency errors and
  otherwise appear successful. Reviewed guards must keep successful checks quiet
  on stderr; warnings are deliberately treated conservatively, not ignored.
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
- For manually configured helper commands, changes to which script a Claude hook
  references require reviewing/updating the mapping. The opt-in definition adapter
  below can reconcile references within an explicitly reviewed allowlist.
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

## Ongoing reviewed definition sync (source builds)

Use `file-guard-config` for one reviewed **path-only** Claude guard in a project.
It shares the script reference, not the whole hooks object or script contents.
For example, add this resource to a profile with its own `version`, `stateDir`,
and `resources` array; replace every absolute path before reviewing the plan:

```json
{
  "id": "path-policy",
  "kind": "file-guard-config",
  "scope": "project",
  "portable": true,
  "allowReformat": true,
  "claude": "/absolute/project/.claude/settings.json",
  "codex": "/absolute/project/.codex/hooks.json",
  "fileGuard": {
    "bridgeExecutable": "/absolute/agent-bridge",
    "scripts": [
      "/absolute/project/tooling/path-policy.sh",
      "/absolute/project/tooling/alternate-reviewed-policy.sh"
    ]
  }
}
```

Only include scripts you have actually reviewed for the helper's input and failure
contract. The bridge does not infer that arbitrary shell code is path-only. Start
with a Claude `PreToolUse` command using exactly `Edit|Write|NotebookEdit`, pointing
to one listed script. Accepted command spellings are a safe absolute executable
path, its shell-single-quoted form, or `"$CLAUDE_PROJECT_DIR"/safe/relative/path`.
Shell arguments, pipelines, async execution, and extra selected-handler fields
are rejected. Other hooks and settings stay native-local.

The generated Codex handler uses `^apply_patch$`, a safely shell-quoted helper
command, and a fixed 15-second host timeout. The Claude timeout, including its
absence, remains local. Both native configuration paths must be the standard
unlinked paths in the same project; scripts must be inside that project. Dependency
paths must be clean and absolute and cannot overlap any managed destination.
The executable and scripts are not installed, copied, or recovery write targets.

After initial sync, a reference change to another already-reviewed script can flow
in either direction through normal `plan`, `sync`, and applying `watch`. The shared
value is `{"script":"/absolute/project/reviewed-script"}`. Concurrent divergent
changes conflict; malformed definitions, unknown wrappers, duplicate selected
handlers, and deletions block. An existing Claude settings file must contain the
selected guard; the adapter will not infer which unrelated hook to replace.
Expanding the allowlist changes resource identity and requires a new ID with
reviewed adoption, not an unnoticed permission expansion.

This explicit resource replaces the conventional whole-hook mapping for the exact
same pair of files. Do not run both adapters against those files. JSON formatting
may change when a reference changes, but unrelated settings values, handler order,
and native timeout values are preserved. Initial Claude adoption is byte-preserving.

Definition synchronization **does not activate hooks**. Review/trust generated or
changed definitions through Codex's normal `/hooks` workflow; the bridge never
changes project trust or hook approvals. Script contents are not hash-pinned: edits
to a script at the same path affect its next execution without changing the shared
reference. Review those edits separately. This remains a bounded policy adapter,
not equivalent notebook behavior, a shell-write guard, or complete cross-host
permission translation.

Tests cover reference round trips, local overlays, conflicts and reviewed
resolution, rollback, dependency ownership, convention overrides, drift rejection,
and actual Codex execution of generated project hooks in disposable fixtures.
