# Skill argument compatibility boundary

Standalone skill argument substitution is not currently portable between the
verified hosts. Claude Code replaces `$ARGUMENTS` inside `SKILL.md` with the full
invocation argument string. Codex loads that same token literally. Copying the
file, preserving its frontmatter, or adding an invocation policy sidecar does not
provide equivalent argument handling. Automatic convention adoption therefore
continues to reject dynamic skill bodies without modifying any peer.

The official [Claude argument contract](https://code.claude.com/docs/en/skills#pass-arguments-to-skills)
documents substitution. The [Codex skill contract](https://learn.chatgpt.com/docs/build-skills)
describes loading skill instructions and explicit/implicit invocation, without
an equivalent argument expansion contract. Codex does document `$ARGUMENTS` in
[custom prompts](https://learn.chatgpt.com/docs/custom-prompts), but that feature
is deprecated, user-global, and explicitly invoked as `/prompts:name`. It is not
a replacement for project-scoped, implicitly discoverable skills. Its positional
indices and dollar escaping also differ from Claude's.

## Installed-host evidence

`TestNativeStandaloneSkillArguments` uses a synthetic standalone skill with one
`$ARGUMENTS` token and arguments containing quoted words plus a literal `$1`.
Claude Code 2.1.268 sends the expanded body, retaining the quotes and literal
argument content. Codex CLI 0.153.4 sends the original body including
`$ARGUMENTS`. The proposed identical-expansion assertion passed for Claude and
failed for Codex before the test was made a differential characterization.

Run it with:

```sh
AGENT_BRIDGE_NATIVE_TESTS=1 go test ./internal/bridge -run '^TestNativeStandaloneSkillArguments$' -count=1 -v
```

The test bypasses bridge adoption only to inspect native loading, uses disposable
homes and configuration roots, and captures requests through loopback fake
providers. It makes no authenticated model calls and does not certify model
interpretation, other host versions, or the desktop/IDE invocation paths.
Existing plugin-command expansion fixtures cover a different importer, which
can omit unsupported command files entirely.

## Usable manual alternative, explicitly non-equivalent

Keep a dynamic skill excluded from automatic portable synchronization. In Codex,
direct it to read the existing source skill and provide the task explicitly:

```text
Read .claude/skills/example-feature/SKILL.md and follow its workflow.
For this invocation, interpret the $ARGUMENTS token as this request:
<feature request>
```

This preserves the source body and host-local settings and makes the workflow
available without enrolling it as equivalent. It relies on model interpretation;
it does not perform native token substitution and does not carry Claude's tool
grants or execution behavior to Codex. A future native adapter must establish
argument expansion, quoting, missing arguments, literal tokens, invocation scope,
and reverse-edit/history behavior before enabling automatic synchronization.
