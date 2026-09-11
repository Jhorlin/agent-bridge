package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

// Characterization only: copying Claude rules or placing nested AGENTS.md files
// does not imply the same native file-read trigger. All content is synthetic.
func TestNativeScopedRuleLoadingCharacterization(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%v", host, nested), func(t *testing.T) {
				f := newFixture(t)
				tools := nativeTools(t, f)
				initScopedRuleFixture(t, f)
				f.write("AGENTS.md", "BRIDGE_SCOPE_ROOT\n")
				f.write("src/AGENTS.md", "BRIDGE_SCOPE_NESTED_AGENTS\n")
				f.write("src/target.txt", "BRIDGE_SCOPE_READ_CONTENT\n")
				rule, pattern := ".claude/rules/fixture.md", "src/**"
				if nested {
					rule, pattern = "src/.claude/rules/fixture.md", "*.txt"
				}
				f.write(rule, "---\npaths:\n  - '"+pattern+"'\n---\nBRIDGE_SCOPE_CONDITIONAL\n")
				var calls atomic.Int32
				requests := make(chan string, 4)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/count_tokens") {
						fmt.Fprint(w, `{"input_tokens":10}`)
						return
					}
					if !strings.HasSuffix(r.URL.Path, "/messages") && !strings.HasSuffix(r.URL.Path, "/responses") {
						http.NotFound(w, r)
						return
					}
					body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
					n := calls.Add(1)
					if n > 2 {
						http.Error(w, "fixture call cap", 429)
						return
					}
					requests <- string(body)
					w.Header().Set("Content-Type", "text/event-stream")
					emit := func(event map[string]any) {
						data, _ := json.Marshal(event)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
					}
					if host == "codex" {
						item := map[string]any{"type": "message", "id": "msg", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture complete", "annotations": []any{}}}}
						if n == 1 {
							name, args := "shell_command", map[string]any{"command": "/bin/cat src/target.txt"}
							if strings.Contains(string(body), `"name":"exec_command"`) {
								name, args = "exec_command", map[string]any{"cmd": "/bin/cat src/target.txt", "max_output_tokens": 100}
							}
							encoded, _ := json.Marshal(args)
							item = map[string]any{"type": "function_call", "id": "tool", "call_id": "scope-read", "name": name, "arguments": string(encoded)}
						}
						emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "fixture"}})
						emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
						emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "fixture", "status": "completed", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
						return
					}
					emit(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg", "type": "message", "role": "assistant", "content": []any{}, "model": "fixture", "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}})
					block, reason := map[string]any{"type": "text", "text": ""}, "end_turn"
					if n == 1 {
						block, reason = map[string]any{"type": "tool_use", "id": "scope-read", "name": "Read", "input": map[string]any{}}, "tool_use"
					}
					emit(map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
					if n == 1 {
						input, _ := json.Marshal(map[string]string{"file_path": f.path("src/target.txt")})
						emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
					} else {
						emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "fixture complete"}})
					}
					emit(map[string]any{"type": "content_block_stop", "index": 0})
					emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}})
					emit(map[string]any{"type": "message_stop"})
				}))
				defer server.Close()
				if host == "codex" {
					f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
					nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Read the fixed fixture file.")
				} else {
					nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "2", "--no-session-persistence", "--setting-sources", "user,project", "--allowedTools=Read", "--", "Read the fixed fixture file.")
				}
				if len(requests) != 2 {
					t.Fatalf("expected before/after read requests, got %d", len(requests))
				}
				before, after := <-requests, <-requests
				if strings.Contains(before, "BRIDGE_SCOPE_CONDITIONAL") || strings.Contains(before, "BRIDGE_SCOPE_READ_CONTENT") || !scopedReadResultContains(after, "BRIDGE_SCOPE_READ_CONTENT") {
					t.Fatal("conditional rule loaded before a read or fixture read failed")
				}
				if strings.Contains(after, "BRIDGE_SCOPE_CONDITIONAL") != (host == "claude") {
					t.Fatal("native conditional-rule loading contract changed")
				}
				if host == "codex" && (!strings.Contains(before, "BRIDGE_SCOPE_ROOT") || strings.Contains(after, "BRIDGE_SCOPE_NESTED_AGENTS")) {
					t.Fatal("native directory instruction loading contract changed")
				}
				t.Log("Conditional rule absent before read; after read present only in Claude. Codex nested instructions are not automatically loaded by this shell read.")
			})
		}
	}
}

func TestNativeCodexNestedInstructionStartupCharacterization(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	initScopedRuleFixture(t, f)
	f.write("AGENTS.md", "BRIDGE_SCOPE_ROOT\n")
	f.write("src/AGENTS.md", "BRIDGE_SCOPE_NESTED_AGENTS\n")
	server, requests := nativePluginFixtureProvider(t)
	f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
	nativeRun(t, f, tools["codex"], "exec", "--cd", f.path("src"), "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
	if len(requests) != 1 {
		t.Fatalf("expected startup request, got %d", len(requests))
	}
	body := <-requests
	root, nested := strings.Index(body, "BRIDGE_SCOPE_ROOT"), strings.Index(body, "BRIDGE_SCOPE_NESTED_AGENTS")
	if root < 0 || nested <= root {
		t.Fatal("native startup did not include ancestor and working-directory instructions in order")
	}
}

func initScopedRuleFixture(t *testing.T, f *fixture) {
	t.Helper()
	cmd := exec.Command("git", "-c", "init.templateDir=", "init", "--quiet", f.dir)
	cmd.Dir = f.dir
	cmd.Env = append(nativeEnvironment(f), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	must(t, cmd.Run())
}

// Require evidence from the requested tool result, not a marker elsewhere in
// the prompt, tool definitions, or unrelated tool output.
func scopedReadResultContains(body, marker string) bool {
	var doc any
	if json.Unmarshal([]byte(body), &doc) != nil {
		return false
	}
	var visit func(any) bool
	visit = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			if (x["type"] == "function_call_output" && x["call_id"] == "scope-read") || (x["type"] == "tool_result" && x["tool_use_id"] == "scope-read") {
				content := x["output"]
				if x["type"] == "tool_result" {
					content = x["content"]
				}
				encoded, _ := json.Marshal(content)
				return strings.Contains(string(encoded), marker)
			}
			for _, child := range x {
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(doc)
}
