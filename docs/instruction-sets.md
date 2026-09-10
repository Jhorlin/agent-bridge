# Multiple instruction sources at one directory

Claude Code loads both `CLAUDE.md` and `.claude/CLAUDE.md` when both exist.
Codex selects one instruction file per directory. Copying just one Claude file
therefore loses instructions. Project conventions now select `instruction-set`
when a canonical `.claude/CLAUDE.md` exists, including alternate-only directories.

The adapter keeps both Claude files separate and generates one `AGENTS.md`:

```markdown
<!-- agent-bridge:source:root:start -->
Instructions from CLAUDE.md, preserved exactly.
<!-- agent-bridge:source:root:end -->
<!-- agent-bridge:source:alternate:start -->
Instructions from .claude/CLAUDE.md, preserved exactly.
<!-- agent-bridge:source:alternate:end -->
```

Edit **inside** the generated sections in Codex. Edits return to their respective
Claude files. An absent root source has no root section and is not created on
first synchronization. Empty files remain distinguishable from missing files.
Original line endings and trailing-newline presence round-trip; each native file
keeps its own permission mode. Shared state stores structured JSON, not Markdown.

Project convention setup requires no new option. An explicit profile can use:

```json
{
  "id": "service-instructions",
  "kind": "instruction-set",
  "scope": "project",
  "portable": true,
  "claude": "services/example/CLAUDE.md",
  "codex": "services/example/AGENTS.md"
}
```

The companion is derived as `services/example/.claude/CLAUDE.md`; it is included
in overlap checks, diagnostics protection, observations and recovery ownership.
The native paths must be an unlinked, same-directory project pair.

## Safety and adoption

- Existing plain `AGENTS.md` content is not silently assigned to a source.
  Review it before adopting a set; unmarked content blocks planning.
- Adding an alternate source to an already tracked ordinary instruction pair
  changes its identity. Preserve the old profile/state and review adoption into
  a fresh state directory; a running watcher does not silently migrate it.
- Section deletion, source-file deletion, malformed/duplicate/reordered markers,
  unmarked Codex text and reserved markers inside source text block writes.
  New content must remain within the source sections.
- Real instruction imports, unsafe links and noncanonical filename casing remain
  blocked. Excluding the ordinary pair excludes the set. Independently excluding
  a tracked companion is rejected; excluding an alternate before adoption leaves
  it native-local and outside the bridge.
- Divergent cross-host edits conflict; there is no last-writer-wins merge.
- Both Claude outputs are journaled together, including unchanged companions.
  Historical Claude restoration requires a complete recorded companion set;
  otherwise select a complete shared or Codex historical version. Recovery never
  uses an arbitrary path from source text.

This adapter preserves instruction content and ordering, not all host behavior.
Codex's combined instruction-size limit, Claude's comment handling, private local
instructions, conditional rules and model interpretation remain host-specific.

Codex defaults to 32 KiB across the root-to-working-directory instruction chain.
For a larger reviewed chain, set a suitable limit at the **top level** of the
project's `.codex/config.toml`, for example `project_doc_max_bytes = 131072`.
Project configuration is loaded only in a trusted project. Start a new native
session and verify full loading; syncing files does not change host trust or
raise this setting automatically. A loopback native regression test reproduces
default truncation and verifies the larger trusted-project setting.

Contracts: [Claude memory](https://code.claude.com/docs/en/memory),
[Codex instruction discovery](https://learn.chatgpt.com/docs/agent-configuration/agents-md).
