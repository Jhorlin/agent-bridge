# Retiring a resource without deleting files

Stop the profile's watcher/service, then review and apply:

```sh
agent-bridge review-retirement /absolute/profile.json RESOURCE_ID
agent-bridge apply-retirement /absolute/profile.json OBSERVATION RESOURCE_ID
```

Use the observation returned by review. The command removes a locally declared
resource, or disables an inherited resource in this profile. If a local override
also has an inherited definition, that definition is disabled too. Parent files,
relative paths and inheritance are preserved; the edited JSON is reformatted.
Retirement is refused if another profile in the ownership roster inherits this
selected profile (directly or indirectly). Retire in a leaf profile, or explicitly
review and remove the dependency first. This prevents invalid child `disable`
entries and a broken coordinator roster; children are never silently rewritten.
Unregistered dependents cannot be discovered automatically: review their impact
before retiring from an uncoordinated/shared parent.

Every native/shared file, pinned symlink, plugin-agent export, baseline and old
backup remains in place. Hosts may still load these files; retirement is **not**
uninstall, disabling a native feature, revoking trust, or removing credentials.
Installed plugin caches are untouched. Ownership of retired native paths is no
longer claimed by this profile; its state directory remains reserved.

Retained manifest identities keep ordinary and supporting-file history listings
readable after retirement. Active resources can still be restored from mixed
journals that also contain retired resources. Retired resources remain read-only:
reactivate their reviewed declarations before attempting historical restoration.

The only committed edit is an atomic replacement of the selected profile. An
exact, private copy of the original profile is saved first at the returned
`backup` path under `stateDir/retirement-backups/UUID/profile.json`. Backups can
contain sensitive configuration: keep them private. A failed attempt can leave
a backup without changing the profile. No separate pending journal is needed
for this single-file operation; it does not promise durability across power loss.

Reviews bind the profile chain, manifest, roster and enrolled profile-chain bytes.
Pending sync/supporting-file
recovery blocks retirement; coordinated profiles also enforce enrollment and
ownership under the common lock. Loaded writers check their configuration again
under the state lock, so an old watcher cannot resurrect a retired resource.
Restart it with the updated profile. Older bridge binaries lack this stale-writer
guard: stop them before making this change. Uncoordinated external file editors
are not governed by bridge locks.

To undo, stop writers, compare the exact profile backup with the current profile
and restore only the intended declaration/disable change. Do not blindly overwrite
newer profile edits. Retained baselines make reactivation drift-aware; review the
plan before syncing. Reusing an old ID for different paths still fails identity
validation. No automatic retirement undo or native-file deletion is provided.
