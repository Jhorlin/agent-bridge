# Strict common skill metadata

`skill-directory` defaults to exact byte copying, retaining the existing schema-1
behavior. Set both `portable: true` and `allowReformat: true` to opt into strict
common metadata. This mode applies to the root `SKILL.md`; supporting files still
use byte-level reconciliation and retain executable bits.

This page describes strict common metadata without additional policy options.
For the separately opted-in `translateSkillInvocation` extension and its paired
Codex sidecar, see [skill invocation policy](skill-invocation.md).

```json
{
  "version": 1,
  "stateDir": ".agent-bridge-strict",
  "resources": [{
    "id": "review-skill-strict",
    "kind": "skill-directory",
    "scope": "global",
    "portable": true,
    "allowReformat": true,
    "claude": "sandbox/claude/skills/review-demo",
    "codex": "sandbox/codex/skills/review-demo"
  }]
}
```

The same option works for project-scoped resources and inherited profiles. Scope
does not discover or enroll skills: register one directory per resource.

## Accepted subset

The bridge deliberately accepts a narrower contract than either host:

- YAML frontmatter must start on the first line and contain exactly the string
  fields `name` and `description`. Both are required.
- Names have at most 64 characters: lowercase ASCII letters, digits and single
  internal hyphens. Descriptions are nonblank, single-line strings of at most
  1,024 Unicode characters.
- UTF-8 instruction text must be nonblank. Its bytes, including line endings,
  remain unchanged; the bridge does not interpret or execute it.
- Duplicate keys, aliases, anchors, custom tags, binary values, unknown fields,
  extra YAML documents and malformed input block planning and syncing.
- `agents/openai.yaml` is rejected on any peer, including a tracked missing
  sidecar. It may contain host-specific invocation controls and dependencies;
  silently copying it to Claude would not translate those controls.

The common store remains an ordinary `SKILL.md`, not a new JSON format. Metadata
is compared semantically across all three peers. Reordering fields or changing
quotes/comments alone causes no writes. When a semantic change requires writing
a peer, its frontmatter is regenerated and comments/formatting may be lost. Body
bytes remain exact. Concurrent different metadata/body edits conflict as one
`SKILL.md` unit; independent supporting-file edits can merge.

## Boundaries and adoption

This mode does not map models, tool permissions, argument substitution, dynamic
shell context, invocation policy, subagent execution, UI metadata or dependencies.
It does not prove instructions/scripts mean the same thing in both tools. Review
these manually before setting `portable: true`; the body is not a semantic linter.
Plugin-bundled skill copying does not opt into this standalone mode.

Do not toggle the mode on an already tracked resource and reuse its baseline.
`allowReformat` is part of resource identity: the planner rejects that change.
Stop any watcher for the old profile, preserve its state/backups, and use a new
resource ID or fresh profile/state directory. Review initial differences with
`audit` and `plan` before applying. Never run old and new profiles against the
same native paths concurrently. Initial differing semantic contents require
manual reconciliation; switching modes does not choose a winner.

## Evidence

[Codex skill documentation](https://learn.chatgpt.com/docs/build-skills) requires
name/description and describes its optional sidecar. [Claude skill documentation](https://code.claude.com/docs/en/skills)
allows additional controls with host-specific behavior; those are excluded here.
The bridge's narrower validation limits are compatibility policy, not a claim
that both hosts enforce these exact limits.

Tests cover forward/reverse propagation, formatting stability, body-byte and
executable-bit preservation, independent supporting-file edits, conflicts,
deletions, unsupported/malformed metadata, unchanged raw mode, identity changes,
exact rollback, and fuzzed normalization round-trips. Native temporary fixtures
verify Codex discovery and Claude receiving generated skill descriptions through
a loopback fake provider (Claude 2.1.266; Codex 0.153.4, 2026-09-08). These are
loading checks, not real-model behavior or permission-equivalence tests.
