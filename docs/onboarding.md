# Global and project onboarding

Discovery is read-only and opt-in per root. It inventories filenames and metadata,
not native file contents. It does not import, enroll, approve, or execute anything.

```sh
agent-bridge discover /absolute/home --global
agent-bridge discover /absolute/project --project
agent-bridge init /absolute/profiles/bridge.json
```

The profile parent directory must already exist. `init` creates a private (0600),
empty profile and refuses to overwrite an existing file. It does not create host
settings or synchronization state.

Discovery looks for conventional instruction files, skill directories, individual
agent files and hook settings. Project discovery also offers MCP configuration.
Global MCP is deliberately excluded because Claude's `.claude.json` also contains
unrelated account and trust state. Installed plugin caches, credentials, histories,
automatic memories, and unregistered project trees are not scanned. Custom host
configuration roots require manually specified profile paths. Symlink candidates
are rejected, not followed.

Candidates are suggestions, not a runnable profile. A settings/config filename
does not prove it contains portable hooks or MCP servers, and skill directories
are not validated during discovery. Review contents, copy selected candidates into
the profile's `resources` array, and provide the required explicit portability,
reformatting consent and MCP server allowlist. See [adapter requirements](adapters.md)
and [instructions, agents and hooks](portable-adapters.md).

Instruction candidates default to whole-file sharing. Choose `instruction-file`
and prepare shared markers if host-specific instructions must remain separate.
Never enroll credentials, tokens or host-specific security settings.

Run `agent-bridge audit PROFILE --json` and `agent-bridge plan PROFILE` before
`agent-bridge sync PROFILE`. Differing initial copies require reconciliation;
discovery never chooses a winner. After a successful sync, `watch PROFILE` previews
changes and `watch PROFILE --apply` applies non-conflicting changes while running.
There is no background service installed automatically.
