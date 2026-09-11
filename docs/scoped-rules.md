# Conditional rules and nested instructions

Claude path-scoped rules are not equivalent to Codex nested `AGENTS.md` files.
Agent Bridge reports this boundary rather than flattening conditional rules into
unconditional instructions or claiming that copying a file reproduces its trigger.

## Native characterization

Disposable fixtures on Claude Code **2.1.268** and Codex CLI **0.153.4** established:

| Invocation | Observed behavior |
| --- | --- |
| Claude starts at root, then reads a matching file | Conditional rule is absent initially and included after the native Read tool runs |
| Claude reads `src/target.txt` with `src/.claude/rules/fixture.md` selecting `*.txt` | The nested conditional rule is included after that read |
| Codex starts at root, then reads a child file with its shell tool | Root instructions are present; Claude rules and child `AGENTS.md` are not automatically added by that read |
| Codex starts inside the child directory | Root and child `AGENTS.md` are present in ancestor-to-child order |

These tests capture real host requests through loopback fake providers. They
require the requested tool result to contain the fixture file's content, not
just a marker somewhere in the prompt. Git initialization, native homes and
configuration are isolated; no real credentials or model requests are used.

```sh
AGENT_BRIDGE_NATIVE_TESTS=1 go test ./internal/bridge -run '^TestNative(ScopedRuleLoading|CodexNestedInstructionStartup)Characterization$' -count=1 -v
```

See the native contracts for [Claude path-specific rules](https://code.claude.com/docs/en/memory#path-specific-rules)
and [Codex directory instructions](https://learn.chatgpt.com/docs/agent-configuration/agents-md).
The shell-read result does not establish every possible Codex tool's behavior,
nor desktop/IDE behavior or future CLI versions.
The nested fixture establishes this positive match, not exhaustive glob or
negative-match semantics; consult Claude's contract for selector ownership.

## Practical boundary

Starting Codex in the relevant directory loads its ancestor instructions, but
does not reproduce arbitrary glob selectors or Claude's read-time activation.
Explicitly asking Codex to read applicable original rule files is a manual,
model-interpreted fallback, not equivalent policy enforcement.

Do not generate `AGENTS.md` inside `.claude/rules`: Claude may load it as another
rule. Do not overwrite an existing project's child instructions or remove path
selectors. A future fallback must remain Codex-only, preserve rule ownership and
selectors, avoid reverse-adoption into Claude, and clearly distinguish guidance
from native activation and security policy.
