# Duplicate and native-variant preflight

Before global onboarding, inspect both hosts without copying, installing,
enrolling, approving, or executing anything:

```sh
agent-bridge inventory-skills /absolute/home
agent-bridge inventory-skills /absolute/home --include-plugin-cache
```

Both commands write JSON to stdout only. They do not create diagnostic logs,
state, backups, profiles, symlinks, or native configuration. Exit 0 means the
inventory completed, **not** that synchronization is safe; exit 2 means one or
more skill candidates could not be inspected; exit 1 means invalid arguments,
unsafe collection roots, read errors or output failure.

## What is inspected

- Personal skills in `.claude/skills` and `.agents/skills`.
- Codex legacy `.codex/skills` and bundled `.codex/skills/.system` candidates.
- With explicit opt-in, `.claude/plugins/cache` and `.codex/plugins/cache`, at
  `marketplace/package/version/skills/name`. All cached versions are retained.
  Hidden and `temp_` staging directories at the cache root are excluded.
  Containers without a `SKILL.md` (such as `skills/v1`) are searched up to four
  additional directory levels; traversal stops at each skill root. Deeper or
  empty candidates are reported blocked, not silently treated as absent.
- Skill frontmatter names, optional `metadata.version`, and matching
  `skill-release.json` release version/channel.
- A sibling `.codex-plugin/plugin.json` with a valid name is reported as native
  Codex manifest evidence. It is not a manifest validation or runtime test.

Plugin caches are **not installation registries**. A cache entry may be inactive,
old, disabled, or superseded. The command deliberately does not read account
files, plugin enablement settings, credentials, marketplace catalogs, or arbitrary
manifest-referenced directories. Nonstandard roots/layouts and plugins without
conventional skill directories require separate review. It does not discover
remote upstream variants that have not been downloaded.

Names are candidate matching keys, not universal package identities. The report
retains host, source, path and all occurrences to avoid conflating unrelated
same-name skills. Metadata is evidence, not an instruction to run an installer.

## Match states

| State | Meaning and next action |
| --- | --- |
| `already-shared` | Both hosts point directly to the same safe personal skill directory. Preserve the link; no copy needed. |
| `identical-copies` | Inspected trees match across hosts. Do not duplicate; choose one owner before enabling ongoing synchronization. |
| `different-copies-review` | Both hosts have the name, but files, modes, or release metadata differ. Preserve both; review version skew, intentional variants and edits. |
| `same-host-collision` | Multiple personal/system locations expose the same name within one host. Review native precedence. |
| `cached-candidate-review` | At least one plugin cache candidate exists. Verify native installation and ownership before creating another copy. |
| `single-host-review` | Only one inspected host/location found. Review portability and native alternatives; absence here is not proof that no native version exists. |
| `inspection-blocked` | Missing/malformed skill, unsafe link, unsupported file, read failure or size limit. Do not assume it is absent and overwrite it. |

The group states prioritize blocked inspections and cache uncertainty. Inspect
the individual entries for duplicate occurrences even when a group has another
status. Release/channel differences are never automatically resolved by choosing
the newest version. System and plugin-manager-owned files remain under their
native installer; this command never generates a runnable profile.

## Safety and limits

Only direct personal skill-directory symlinks into one of the three personal
skill collections under the selected home can be inspected. Linked parents,
outside-root targets, chained links, cache links and nested supporting-file links
are rejected. Files are opened without following the final symlink and checked
for changes during reads. This is a read-only point-in-time inventory, not a
transactional tree snapshot or protection from concurrent malicious parent swaps.

Digests cover regular supporting-file paths, bytes and executable bits. `.git`
and `node_modules` directories are excluded; empty directories and timestamps do
not affect equality. Identical digests do not prove dependency or host behavior
parity. A file is limited to 8 MiB; an inspected skill tree to 64 MiB and 20,000
entries. Collection/cache traversal is bounded to 20,000 entries. Reports include
local paths, names and version metadata but never skill bodies or script contents;
review metadata before sharing a report externally.

## Relationship to synchronization

This is a separate, advisory preflight. It **does not change existing profiles or
add automatic exclusions to `sync`/`watch`**. Run it before creating a global
profile, preserve installer-owned variants, explicitly exclude those paths from
conventions, and run the ordinary `audit`/`plan`/reviewed-enrollment workflow for
the remaining portable resources. Re-run inventory after native installations
or updates. An already-running profile does not automatically gain this check.

Global profiles can explicitly opt into an ongoing native-candidate guard with
`conventions.protectNativeSkills: true` (requires global scope and the skills
feature). On each configuration load, including watcher reloads, it inventories
personal, legacy, system and conventional cached skills. An automatically
discovered skill is blocked if its declared name has another candidate outside
its Claude/Codex pair, or a matching candidate cannot be inspected. Directory
aliases do not bypass name matching. Explicitly reviewed exclusions remain
excluded; this guard never chooses, deletes, upgrades or disables a variant.

This is conservative: even an inactive cached candidate requires review. It
does not guard explicit resource exceptions or prove skill portability, and is
not atomic with another installer changing files concurrently. Hashing skill
trees adds configuration-load latency. Leave installer-owned variants excluded
and managed through their native installation mechanism. Existing profiles keep
their behavior unless they opt in.

The guard also validates prospective skill names from the shared store,
historical restores and reviewed conflict resolutions, including collisions
between two proposed personal skills. Validation runs during previews and again
before a transaction creates its journal or writes native files. A previously
reviewed restoration does not bypass a newly installed native candidate. These
checks remain point-in-time checks, not a lock on an external installer.

Regression tests use disposable fixture homes: shared links, identical copies,
supporting-file and executable-bit differences, release/channel skew, system
collisions, multiple cached versions, native manifest evidence, content redaction,
malformed input, bounded reads, unsafe links, ignored account files, deterministic
output and CLI exit behavior. They do not certify installed plugin execution.
