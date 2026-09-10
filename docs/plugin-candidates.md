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

## Ongoing protection for global authoring packages

After reviewing native ownership, a global convention profile can opt into:

```json
{
  "version": 1,
  "stateDir": "./state",
  "conventions": {
    "root": "/absolute/selected-home",
    "scope": "global",
    "features": ["plugins"],
    "protectNativePlugins": true
  },
  "resources": []
}
```

The guard reads only the three known manifest locations under
`.claude/plugins/cache/{marketplace}/{package}/{version}` and
`.codex/plugins/cache/{marketplace}/{package}/{version}`, relative to that
explicit root. Package bodies, registry settings, credentials, enablement and
trust are not inspected or changed. It compares declared names, not directory
aliases, across every known manifest, including packages with multiple manifests.

A same-name cached candidate blocks automatic adoption, even if it is inactive,
an old version, or has a different publisher. This is conservative collision
protection, not automatic counterpart selection. Use the comparison command and
upstream evidence to review ownership, then exclude installer-owned authoring
packages through the ordinary reviewed profile/enrollment workflow. Do not enroll
the cache itself. No existing cache or authoring package is deleted by the guard.

Checks run at discovery, planning, history/conflict review and before a write
journal is created. They also reject two automatic authoring packages with the
same prospective name. Malformed/oversized identity files, unknown schemas,
symlinks in inspected paths, unexpected cache-level files, missing manifests and
inventories exceeding 20,000 directory entries fail closed with private errors.
Hidden/staging directories are not silently exempted; an incomplete installation
can pause a guarded profile until its native installer finishes.

This option requires global scope with plugin conventions enabled. It does not
cover explicit resources, project profiles, custom cache roots, custom package
depths, external development directories or differently named counterparts.
Absent caches do not prove that no native counterpart exists. Scanning is not an
atomic snapshot or a native installer lock; keep installers stopped during
reviewed writes. Existing profiles retain their behavior unless they opt in.

Recovery also loads the guarded profile: a newly cached collision can pause
recovery after a crash. The pending journal remains intact; resolve ownership
privately before retrying recovery. The guard never clears pending state or
disables itself to get past an error.
