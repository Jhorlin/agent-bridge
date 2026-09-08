# Working on Agent Bridge

- Never test against a real user home or real Claude/Codex configuration; use temporary fixtures.
- Run `npm test` and `npm run check` before handing off changes.
- Never commit credentials, local config, generated state, or backup contents.
- Treat unsupported or lossy translations as explicit errors/reports, not successful synchronization.
- Preserve conflicting edits. Never introduce last-writer-wins behavior as a default.
- Keep README capability claims aligned with tested implementation.
