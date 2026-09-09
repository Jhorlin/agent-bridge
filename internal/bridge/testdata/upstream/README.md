# Public upstream regression fixtures

These are inert configuration data, not installable plugins or skills. Tests never
execute the upstream commands, start their servers, install their packages, or
fetch anything from the network. The `bun` command in the MCP sample is data;
Agent Bridge's build/test/runtime does not require Bun or Node.

`catalog.json` records repository, exact commit/path/Git blob, fixture SHA-256,
license, transformations, workstream and expected current result. The three
configuration files are byte-identical upstream copies, checked offline against
their Git blob hashes. Public contents were inspected: no credentials or personal
configuration were imported. This small initial set is not coverage of all eight
workstreams. Passing an expected-rejection test does not mean support is implemented.

## Attribution

- `fakechat/mcp.json`, `fakechat/plugin.json`, and `fakechat/LICENSE` come from
  [Anthropic's fakechat plugin](https://github.com/anthropics/claude-plugins-official/tree/517b2fcd1b60fa2181ac52dcf8492361ba341180/external_plugins/fakechat).
  Copyright 2026 Anthropic, PBC. Apache-2.0; the original license is retained.
  Configuration files are renamed only. The license has a terminating newline
  added. No server code, package manifests, lockfiles or scripts are included.
- `cli-creator/openai.yaml` and `cli-creator/LICENSE` come from
  [OpenAI's CLI Creator skill](https://github.com/openai/skills/tree/49f948faa9258a0c61caceaf225e179651397431/skills/.curated/cli-creator).
  Apache-2.0; the original license is retained. Files are renamed/relocated only;
  no skill instructions, scripts or references are imported.

No separate NOTICE file was present within either selected component at these
revisions. Agent Bridge's MIT license does not replace these upstream licenses.

Future user-derived fixtures require a separate read-only source selection,
private staging, an explicit field-level sanitization record, and review before
public commit. Regex-only secret removal is not sufficient. Preserve structural
differences; replace identities, private text, credentials, endpoints and execution
side effects where needed. Record those modifications and their test purpose.
