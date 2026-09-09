# Skill invocation policy (opt-in source builds)

A new standalone `skill-directory` resource can opt into:

```json
{
  "id": "demo",
  "kind": "skill-directory",
  "scope": "global",
  "claude": "/absolute/claude/skills/demo",
  "codex": "/absolute/codex/skills/demo",
  "portable": true,
  "allowReformat": true,
  "translateSkillInvocation": true
}
```

Place this resource inside a normal version-1 profile. Project scope works the
same way with explicitly selected project paths. This does not enroll every
skill automatically or alter native trust, credentials, permissions or caches.
Do not test against your real home; first use separate disposable directories.

The shared/Claude `SKILL.md` accepts `name`, `description`, bounded informational
`license`/`compatibility`/`metadata`, and a boolean `disable-model-invocation`.
Missing Claude policy defaults to false. Codex's `SKILL.md` retains common
informational metadata; its `agents/openai.yaml` must contain:

```yaml
policy:
  allow_implicit_invocation: false
```

Claude `disable-model-invocation: true` maps to Codex
`allow_implicit_invocation: false`, and vice versa. This controls automatic
discovery while retaining explicit `/demo` (Claude) or `$demo` (Codex) invocation.
It is not a permission or execution sandbox boundary. Claude documents additional
subagent-preloading and scheduled-task behavior that this translation does not
certify. See the official [Claude skill contract](https://code.claude.com/docs/en/skills)
and [Codex skill contract](https://learn.chatgpt.com/docs/build-skills).

Codex entry and sidecar are one logical, reviewed item (`demo/SKILL.md`), with
separate physical writes in the existing recovery journal. The generated sidecar
is not copied into Claude/shared. Both raw files participate in stale-review
checks. Conflicting body/policy changes block together; no last-writer-wins rule
is introduced. Historical selection of this item restores the selected policy
as well as instructions. A Codex historical version requires its companion
sidecar snapshot from the same transaction.

An existing Codex skill must have an explicit valid sidecar before adoption;
deleting one does **not** mean enabling automatic invocation. A completely absent
Codex skill can be generated from a valid Claude source. An orphan sidecar,
symlink, case variant, unknown YAML field or non-boolean policy blocks writes.
UI metadata, tool dependencies, arguments, allowed tools, invocation visibility,
subagent context and plugin-bundled skill policy are not supported by this mode.
Their rejection is a safety boundary, not a claim of feature completeness.

Instruction body bytes, including CRLF, remain unchanged. Frontmatter and policy
YAML may be reformatted; comments are not retained when those files are rewritten.
Supporting scripts/assets/references retain the existing independent-file behavior.
The policy sidecar cannot be renamed/deleted through supporting-file commands.

Changing this flag changes resource identity. Do not toggle it on an already
baselined resource or erase its baseline to bypass validation. Stop the old
watcher, retain its profile/state/backups, review all peers, and prepare a new
non-overlapping profile/state with explicit adoption consent. Preview/audit the
new profile and resolve any initial disagreement before enabling writes. Old
writers do not understand this option; do not run them against the new profile.

## Evidence

Offline tests cover both directions, repeat sync, unchanged body bytes, conflict
selection, policy-aware history, malformed metadata, stale raw reviews, identity
consent, sidecar deletion/path refusal, every-write rollback and process
interruption with later-edit preservation. A round-trip fuzz target checks every
accepted canonical document across all three sides.

The native matrix uses bridge-generated files in disposable homes with Claude
Code 2.1.266 and Codex 0.153.4. Eight cases cross each host, policy enabled/disabled,
and implicit/explicit invocation. Local fake providers verify discovery-description
visibility and explicit body loading without authenticated or paid model calls.
This does not prove model selection quality, arbitrary scripts, scheduled tasks,
subagent preloading, every host version or universal skill compatibility.
