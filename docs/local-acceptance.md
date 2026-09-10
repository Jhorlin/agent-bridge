# Local acceptance test for the current Go source build

This is a bounded integration test, not a full-parity release certification.
Read the [dated status](interoperability-status.md) first. Do not enroll installed
plugin caches, credentials, trust records, or an entire native configuration
directory as raw shared files.

## Check the profile before writing

Build from the repository and use the intended profile's absolute physical path:

```sh
go build -o agent-bridge ./cmd/agent-bridge
./agent-bridge version
./agent-bridge audit /absolute/profile.json --json
./agent-bridge plan /absolute/profile.json
```

Review the selected resources and proposed writes. A blocked audit or plan is a
stop condition, not permission to delete the source or remove a safety check.
Resolve conflicts explicitly; do not replace populated native counterparts.
On macOS, use physical paths such as `/private/tmp/...` for disposable fixtures;
`/tmp` is a symlink and strict path inspection rejects it.

## Verify both directions on one harmless resource

1. Add a distinctive, harmless instruction to the selected Claude authoring
   file, then run `plan` and `sync` with the same profile. Confirm the expected
   Codex authoring output and an `in-sync` repeat plan.
2. Change that instruction in the Codex authoring file. Repeat plan/sync and
   confirm the reverse change without losing Claude-local settings.
3. Make different edits to that same instruction on both sides. Confirm the
   plan reports a conflict and ordinary sync does not choose a winner. Use the
   documented [reviewed resolution](phase-two.md) workflow.
4. Start fresh native sessions in the project and check instruction/skill
   discovery. Authoring convergence alone does not establish native loading.

Run the native behavior test only on content you have reviewed. Synchronization
does not translate arbitrary task text, model choices, tool grants or security
enforcement. Do not use an external write operation as a smoke test.

## Plugins, hooks and connected services

- If a package already has a native counterpart, first use
  [compare-plugin-candidates](plugin-candidates.md) on the two explicit package
  roots. Differences remain review items; matching names are not equivalence.
- Edit plugin authoring files, not installed caches or migrated command skills.
  Install/refresh through the native host separately and use
  [compare-plugin-copy](plugin-copy.md) to inspect a selected cache copy. Codex
  refresh can re-enable a disabled plugin; Claude same-version updates can keep
  old content. Neither is transparently managed by the bridge.
- Review hooks in each native host before trusting them. First use an inert
  script that records an event in a temporary file. Trust is never transferred.
- Connect Figma, Stripe and Qodo through their own native sign-in flows. Verify
  account identity or another harmless read-only action. A listed/connected MCP
  server or installed plugin alone does not establish a working authenticated
  workflow. Never copy OAuth/session credentials between hosts.

## Capture a reproducible failure

Record the bridge build, both native versions, profile path, exact command and
expected versus observed behavior. Locally inspect:

```sh
./agent-bridge doctor /absolute/profile.json
./agent-bridge logs /absolute/profile.json --tail 100
```

Keep raw logs, native files and backups private. For remote review, create and
review a [sanitized support bundle](diagnostics.md) rather than uploading the
whole project or home directory. Do not call the test complete until the
selected resource passes both authoring directions and its intended native
behavior; unsupported resources remain outside that claim.
