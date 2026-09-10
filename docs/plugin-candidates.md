# Comparing native plugin candidates

Use two explicitly selected package directories to inspect native counterpart
evidence before deciding whether to translate a package:

```sh
agent-bridge compare-plugin-candidates /absolute/left-package /absolute/right-package
```

This read-only command needs no synchronization profile. It reads only known
manifest files (`.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`, and
root `plugin.json`) and filesystem inventory metadata. It does not open skill,
command, agent, hook, MCP, script, or arbitrary configuration bodies. It never
executes scripts, discovers caches, enrolls a resource, installs a package, or
changes native configuration, authentication, enablement, or trust. No logs or
state files are created.

The JSON report contains only fixed labels and counts:

- Manifest locations on each side and presence of known component declarations.
- Every left/right manifest pairing, with `match`, `mismatch`, `left-absent`,
  `right-absent`, or `both-absent` comparisons for name, version, author,
  repository, and homepage. Values themselves are never printed.
- Counts of conventional skill entrypoints, nested command files, flat agent files, hook
  configuration files, and MCP configuration files, including common paths and
  paths present on only one side. Root paths and component names are not printed.

Counts describe files, not supported runtime capabilities. Skill counts include
`skills/**/SKILL.md`; command counts use `commands/**/*.md` including flat files; agent counts use
`agents/*.md` and `agents/*.toml`; hooks count `hooks/hooks.json`; MCP counts
`.mcp.json` and `mcp.json`. Custom manifest-referenced paths are not followed.
Different contents under the same path count as a common path. A declaration
being present does not establish that its value is valid or that a host loads it.

The result always remains `review-required`. Matching names, versions, or
provenance fields do not prove authenticity, equivalence, compatibility,
installation, or that one package should replace another. Missing fields are
reported as missing evidence rather than matching identity. Multiple native
manifests are retained as separate observations, not silently selected or merged.
Review the upstream source and actual native availability before associating
counterparts. This command makes no automatic association or exclusion.

Each manifest is limited to 256 KiB, with strict JSON duplicate-key rejection,
bounded identity fields, and known schema handling. Inventory traversal is
limited to 20,000 entries and 32 path levels; directories are read in bounded
batches rather than materialized before checking the entry budget. Symlinks, special files, ambiguous
manifest casing, malformed known manifests, and unsafe or missing roots fail
closed. Unknown regular files contribute only to the file count; their contents
are never read. Known manifests and inventory metadata are rechecked for
ordinary concurrent changes; this is not an atomic snapshot or installer lock.

Exit 0 means the inspection completed, even when all comparison fields differ.
Exit 1 means invalid arguments, unsafe/incomplete inspection, or output failure.
No error includes the rejected paths or manifest values.

For byte comparison of an already registered authoring package and an explicitly
selected native copy, use [compare-plugin-copy](plugin-copy.md) instead.
