# Working on Agent Bridge

- Never test against a real user home or real Claude/Codex configuration; use temporary fixtures.
- Run `go test -race ./...`, `go vet ./...`, and `go build ./cmd/agent-bridge` before handing off changes. Format Go changes with `gofmt`.
- Preserve config schema 1, manifest schema 2, and recovery-journal schema 1 compatibility; add regression tests when changing their encoding.
- Never commit credentials, local config, generated state, or backup contents.
- Treat unsupported or lossy translations as explicit errors/reports, not successful synchronization.
- Preserve conflicting edits. Never introduce last-writer-wins behavior as a default.
- Keep README capability claims aligned with tested implementation.
