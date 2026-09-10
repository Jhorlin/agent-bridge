# Package-relative hook executables

Source builds support this bounded command in compatibility-layout plugin hooks:

```json
{
  "hooks": {
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": "\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"",
        "timeout": 10
      }]
    }]
  }
}
```

The referenced executable must exist in that authoring package under `hooks/` or
`scripts/`. The bridge synchronizes supporting files and executable bits without
running them. Both native hosts resolve the quoted root token; no machine-specific
cache path is embedded in generated configuration. This is macOS/Linux support,
not a Windows shell or permission mapping.

Only one quoted executable path is accepted. Arguments, interpreters, shell
operators, other environment variables, traversal, symlinks, non-executable or
missing dependencies and references to the hook JSON itself are rejected.
`hooks/hooks.json` must use that exact spelling, including on case-insensitive
filesystems. Existing bounded event, matcher and explicit 1–60 second timeout
requirements still apply. Arbitrary input/output policy semantics are not
translated. Supporting files are not an endorsement of their behavior.

The bridge validates both current definitions and the prospective merged package.
Independent reference/permission edits, historical restores and conflict choices
cannot produce a selected hook with a missing or non-executable dependency.
Reviewed supporting-file deletion, rename and rename undo also refuse to remove
an executable that a current hook still references. Change and synchronize the
hook reference first; the bridge does not rewrite shell commands during rename.
Raw changes also invalidate stale reviews. Native hook trust is never copied,
granted or bypassed by synchronization.

`TestNativePluginRelativeHookExecution` verifies forward/reverse synchronization
and native execution on Claude 2.1.267 and Codex 0.153.4 in disposable paths with
spaces, using a local fake model provider and an inert script. Codex discovers
the hook untrusted and does not run it without fixture-only invocation consent.
Claude local marketplaces can execute from the authoring folder even though
`plugin list` reports a cache directory; the native token handles that distinction.

## MCP and portable-layout limits

The same replacement is **not** valid for every component. Installed Codex 0.153.4
compatibility `.mcp.json` leaves root tokens literal and uses the session cwd.
Portable `mcp.json` can load a relative executable and expand `PLUGIN_ROOT`, but
its default cwd is the installed package root, unlike Claude's session cwd.
Package-relative MCP therefore remains blocked by the bridge pending a tested
format and cwd mapping; do not replace tokens manually and assume equivalence.
Claude 2.1.267 ignores declared MCP `cwd` in both plugin and project definitions.
Source builds reject nonempty common MCP `cwd` instead of claiming an equivalent
mapping. Configure working-directory behavior in a separately reviewed launcher,
or leave that server native-managed outside the synchronized allowlist.

Portable Codex hook loading remains blocked on 0.153.4. The current
[OpenAI package documentation](https://developers.openai.com/plugins/build/plugins)
describes portable hook extensions, but isolated testing of both implicit
discovery and explicit `extensions.com.openai.hooks` found neither loaded on this
installed version. Compatibility hooks do work. Claude documents its root token
in the [plugin reference](https://code.claude.com/docs/en/plugins-reference).
