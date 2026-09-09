# MCP formatting preservation

Source builds attempt a surgical text patch before the consented full-document
renderer. When existing object keys are unchanged and differences are supported
scalar values, only those value tokens change. Surrounding whitespace, key order,
line endings, unrelated values and TOML comments remain byte-for-byte intact.
The replacement scalar may use different quoting. The entire patched document
must parse to exactly the intended complete document before it is accepted.

This is best-effort preservation, not a no-reformat mode. New/removed fields or
servers, changed arrays, inline-table changes and TOML documents containing array
tables fall back to the existing renderer. Missing destination documents are also
generated normally. That renderer preserves unrelated values semantically but
can remove comments and change layout. `allowReformat: true` remains mandatory;
inspect the private backup/diff before accepting a structural change.

This changes neither the MCP transport/security model nor conflict decisions.
Tests cover JSON/TOML scalar patches, quoted/dotted keys, CRLF, multiline strings,
unchanged nested data, structural fallback, forward/reverse sync, idempotence,
injected rollback, and parser fuzzing. No host behavior is inferred from comment
preservation. Formatting-only edits are still not propagated across hosts.
