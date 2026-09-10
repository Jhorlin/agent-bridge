# Native-local skill settings

Opt in with `preserveSkillSettings: true` on a strict invocation-translated
`skill-directory`, or under `conventions` for automatically discovered skills.
The default remains strict rejection. This option shares the portable instruction
projection; it does **not** provide equivalent host permissions or execution.

Claude's `allowed-tools`, `argument-hint`, `version`, and custom `mcp` metadata
remain only in its existing source file. Reverse edits preserve their values.
New Codex files receive no permission grant; existing Codex approval configuration
is not changed. Shared state excludes these fields from the canonical skill, but
private recovery journals can contain complete source snapshots.

`allowed-tools` is a Claude per-turn pre-approval grant, not a tool restriction.
It must not become a persistent Codex approval rule. Dependency names are retained
as metadata, not installed or authenticated. Unknown fields and behavioral fields
such as `context`, `hooks`, and `disallowed-tools` still fail closed. Arguments and
Claude-only body expansions require separate handling.

Existing convention-managed strict skills can opt in without resetting state:
their previous portable projection is unchanged. Downgrading an enrolled resource
is rejected; use the reviewed retirement/adoption workflow when changing identity.
Config schema 1, manifest schema 2 and journal schema 1 remain unchanged. Use a
current binary: older binaries must not be used to write profiles using new fields.

References: [Claude skill permissions](https://code.claude.com/docs/en/skills#pre-approve-tools-for-a-skill)
and [Codex skill metadata](https://learn.chatgpt.com/docs/build-skills#optional-metadata).
