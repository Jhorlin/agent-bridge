# Conventional plugin commands

Source builds synchronize flat `commands/kebab-case.md` files in compatibility
plugin packages. Each file must contain YAML frontmatter with only one string
`description`, followed by a nonempty static instruction body. Names are bounded
to 64 characters. Unknown metadata, arguments/variable interpolation (`$`), shell
preprocessing, nested command directories and portable-root Codex layout are
rejected. Existing `portable: true` is compatibility consent, not execution trust.

Claude consumes these command files directly. In native Codex 0.153.4 testing,
installation migrated `commands/bridge-command.md` to a private cache skill named
`demo:source-command-bridge-command` for plugin `demo`. Claude's invocation is
`/demo:bridge-command`; Codex's is `$demo:source-command-bridge-command`. This is
not identical naming or general argument compatibility. An authoring skill whose
directory would collide with this migrated name is rejected.

Only authoring package files participate in synchronization. Never enroll the
installed cache or edit Codex's generated migrated skill as the reverse source.
Edit `commands/*.md` in either authoring package, synchronize, and refresh the
native installation separately. The bridge does not install, enable or trust it.

Offline tests cover strict parsing, malformed fields, unsafe names, collisions,
forward/reverse changes, repeated sync, conflicts, reviewed choice, deletion
refusal and injected rollback. Native tests use disposable homes and inert text:
Codex discovers and explicitly invokes the generated command as a migrated skill;
Claude installs, validates and expands the reverse-generated command. Both
invocation checks capture the instruction body at local fake providers.
These tests do not certify arbitrary scripts, tool permissions or model outcomes.

## Claude-local command settings

Source builds accept `preserveCommandSettings: true` on a plugin resource with
`allowReformat: true`. Plugin conventions enable this retention automatically.
It retains bounded `model`, `allowed-tools` and `argument-hint` values only in
the original Claude command. They are excluded from shared canonical text and
Codex output. Reverse edits, conflict choices and historical restores preserve
the current Claude-local settings; changing them makes an earlier review stale.

This is not cross-host model, argument or permission translation. The command
body must still be static; `$ARGUMENTS`, shell preprocessing, invocation-control
fields, unknown settings and host-local fields in Codex/shared input still fail.
Codex continues to use its own configured model and permissions. Portable-root
plugins still do not support commands. Native settings are not reconstructed if
the original Claude file is absent.

Explicit profiles without this option keep strict metadata rejection. Existing
automatic plugin baselines may adopt the additive retention mode without a new
resource ID because the previously accepted portable projection is unchanged;
other identity changes remain guarded. Native fixture tests verify command
discovery/invocation text with the overlay enabled using isolated fake providers,
not arbitrary tool behavior or real model results.

The [official plugin overview](https://learn.chatgpt.com/docs/build-plugins)
describes skills/package formats but does not promise identical cross-host command
semantics. The specific migration name above is version-pinned native evidence.
