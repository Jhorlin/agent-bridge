# Checking a selected plugin copy

```sh
agent-bridge compare-plugin-copy /absolute/profile.json bundle codex /absolute/selected/plugin-copy
```

Source builds compare the selected registered native authoring package with one
explicit, separate copy directory. They report missing, changed and unexpected
file names plus content/executable-bit inventory digests, never file contents.
Exit 0 means the compared authoring files match, 2 means differences, and 1 means
invalid/unsafe/unavailable input or output failure. Keep reports private: resource
file names themselves can be sensitive.

The copy must have the expected compatibility/portable manifest for the selected
side. Symlinks, unsafe paths and overlap with managed authoring roots fail. Unknown
extra files are reported by name but their contents are not read. Codex-generated
files under `.codex-plugin/migrated-command-skills/` are counted separately and
excluded from matching; their behavior or integrity is not certified by this
comparison. Normal permission bits other than executable bits are not compared.

This does not discover the active cache, prove a directory is installed, or infer
enablement, authentication or hook trust. Select the actual copy through native
host inspection. Inventories/raw hashes are rechecked, but the result is still a
point-in-time report rather than a lock on host state. No host process, installer,
network call, refresh or native configuration write occurs.

Native Codex 0.153.4 tests verify matching after fixture installation, stale after
source editing/sync, and matching after native uninstall/reinstall in disposable
compatibility and portable-layout packages. This is evidence for the comparison,
not a production install/refresh implementation.

The [official app-server documentation](https://learn.chatgpt.com/docs/app-server)
currently marks `plugin/list`, `plugin/read`, `plugin/install` and
`plugin/uninstall` as under development and advises against production clients.
Agent Bridge keeps those calls inside opt-in isolated acceptance tests. Production
installation/refresh remains native-managed pending a suitable supported contract
and tests preserving host enablement, authentication and trust decisions.
