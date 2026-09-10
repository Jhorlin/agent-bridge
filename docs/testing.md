# Test coverage

Coverage measures executed Go statements, **not Claude/Codex feature parity** or
proof that every input and host version works. Favor regressions that protect
files, privacy and observable behavior over tests written only to raise a number.

## Reproduce locally

From the repository root, with Go 1.25+:

```sh
go test -race ./... -coverprofile=/tmp/agent-bridge-coverage.out
go tool cover -func=/tmp/agent-bridge-coverage.out
go tool cover -html=/tmp/agent-bridge-coverage.out
go vet ./...
go build ./cmd/agent-bridge
```

The ordinary suite uses disposable fixtures. The release compiler test builds
and executes a tiny local Go module with isolated home, cache, temporary and
telemetry directories; module downloads and external cache helpers are disabled.
No authenticated model requests or native service installation are required.
See [native testing](native-testing.md) for separately opted-in host checks.

## September 10, 2026 coverage expansion

Compared with source `4c23af1`, the ordinary suite measured:

| Package | Before | After |
| --- | ---: | ---: |
| Overall | 85.9% | 86.9% |
| Bridge | 87.0% | 87.5% |
| CLI | 81.9% | 85.8% |
| Release helpers | 69.1% | 88.3% |
| Diagnostics | 84.9% | 84.9% |
| Hook guard | 84.1% | 84.1% |

These are rounded statement percentages from `go test ./... -coverprofile=...`.
The expansion adds 23 top-level tests, including table-driven cases, without
changing production behavior:

- Corrupt recovery pointers/journals and invalid snapshots retain native files
  and recovery evidence. A valid recovery control proves the fixture can recover.
- Invalid writes, symlinked parents and destination collisions preserve files
  and leave no temporary output behind; empty files and executable modes round-trip.
- Conflict, stale-review and configuration failures produce useful diagnostics
  without exposing private input. Support bundles reject indirect managed paths.
- Project/global convention onboarding creates only a private profile; unsafe
  roots do not create configuration.
- Missing release inputs, cancellation, invalid builder products and occupied
  artifact names do not overwrite files or publish misleading checksums.
- A real compiler checks version injection, isolated build flags, readonly module
  requirements and private compiler failures.

Review identified and corrected a fixture permission mismatch that could hide a
missing recovery guard. As an additional check, removing after-snapshot validation
in a disposable source copy made all three malformed-snapshot cases fail while
the valid control passed. The real source retained its guard.

## Deliberate limits

We do not force artificial `fsync`/close/entropy failures just to reach 100%, nor
count a skipped native test as a passing host integration. Child-process entry
points are exercised by executable tests but are not instrumented into the
parent's ordinary coverage profile; the command packages therefore report 0%.
Coverage also does not certify hostile filesystem races, power-loss durability,
external authentication, or arbitrary plugin/skill semantics.

New production behavior should bring forward/reverse and rejection tests as
appropriate, including conflict preservation, privacy and recovery checks when
it touches synchronized data. See [compatibility](compatibility.md) and
[interoperability status](interoperability-status.md) for feature-level evidence.
