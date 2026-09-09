package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeBashToolHooks(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		for _, mode := range []string{"observe", "deny", "error", "timeout", "untrusted"} {
			if mode == "untrusted" && host != "codex" {
				continue
			}
			t.Run(host+"/"+mode, func(t *testing.T) {
				f := newFixture(t)
				tools := nativeTools(t, f)
				f.raw.Resources = []resourceInput{{ID: "tools", Kind: "hook-config", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/settings.json", Codex: "codex-home/hooks.json"}}
				f.load()
				hooks := map[string]any{}
				for _, event := range []string{"PreToolUse", "PostToolUse"} {
					script := "#!/bin/sh\n/bin/cat > '" + f.path(event+".json") + "'\n"
					if event == "PreToolUse" && mode == "deny" {
						script += "printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"BRIDGE_FIXTURE_DENY\"}}'\n"
					}
					if event == "PreToolUse" && mode == "error" {
						script += "exit 1\n"
					}
					timeout := 10
					if event == "PreToolUse" && mode == "timeout" {
						script += "/bin/sleep 2\nprintf completed > '" + f.path("timeout-completed") + "'\n"
						timeout = 1
					}
					f.write(event, script)
					must(t, os.Chmod(f.path(event), 0700))
					hooks[event] = []any{map[string]any{"matcher": "^Bash$", "hooks": []any{map[string]any{"type": "command", "command": f.path(event), "timeout": timeout}}}}
				}
				data, err := json.Marshal(map[string]any{"hooks": hooks})
				must(t, err)
				f.write("claude-home/settings.json", string(data))
				f.apply()
				var calls atomic.Int32
				var sawDeny atomic.Bool
				const command = "/usr/bin/printf bridge-tool-fixture"
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
					if strings.Contains(string(body), "BRIDGE_FIXTURE_DENY") {
						sawDeny.Store(true)
					}
					n := calls.Add(1)
					if n > 2 {
						http.Error(w, "fixture call cap", 429)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					emit := func(event any) {
						data, _ := json.Marshal(event)
						var envelope struct{ Type string }
						json.Unmarshal(data, &envelope)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", envelope.Type, data)
					}
					if host == "codex" {
						emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "fixture"}})
						var item any = map[string]any{"type": "message", "id": "msg", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture complete", "annotations": []any{}}}}
						if n == 1 {
							name, args := "shell_command", map[string]any{"command": command}
							if strings.Contains(string(body), `"name":"exec_command"`) {
								name, args = "exec_command", map[string]any{"cmd": command, "max_output_tokens": 100}
							}
							encoded, _ := json.Marshal(args)
							item = map[string]any{"type": "function_call", "id": "tool", "call_id": "bridge-tool", "name": name, "arguments": string(encoded)}
						}
						emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
						emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "fixture", "status": "completed", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
					} else {
						emit(map[string]any{"type": "message_start", "message": map[string]any{"id": "msg", "type": "message", "role": "assistant", "content": []any{}, "model": "fixture", "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}})
						block, reason := map[string]any{"type": "text", "text": ""}, "end_turn"
						if n == 1 {
							block, reason = map[string]any{"type": "tool_use", "id": "bridge-tool", "name": "Bash", "input": map[string]any{}}, "tool_use"
						}
						emit(map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
						if n == 1 {
							input, _ := json.Marshal(map[string]string{"command": command})
							emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
						} else {
							emit(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "fixture complete"}})
						}
						emit(map[string]any{"type": "content_block_stop", "index": 0})
						emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 1}})
						emit(map[string]any{"type": "message_stop"})
					}
				}))
				defer server.Close()
				if host == "codex" {
					f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
					args := []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only"}
					if mode != "untrusted" {
						args = append(args, "--dangerously-bypass-hook-trust")
					}
					nativeRun(t, f, tools[host], append(args, "Run the fixed fixture command.")...)
				} else {
					nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "2", "--no-session-persistence", "--setting-sources", "user", "--allowedTools=Bash", "--", "Run the fixed fixture command.")
				}
				if mode == "deny" && !sawDeny.Load() {
					t.Fatal("denial was not delivered to the local provider")
				}
				if mode == "timeout" {
					f.missing("timeout-completed")
				}
				for _, event := range []string{"PreToolUse", "PostToolUse"} {
					if mode == "untrusted" {
						f.missing(event + ".json")
						continue
					}
					if event == "PostToolUse" && mode == "deny" {
						f.missing(event + ".json")
						continue
					}
					var payload map[string]any
					must(t, json.Unmarshal([]byte(f.read(event+".json")), &payload))
					input, ok := payload["tool_input"].(map[string]any)
					if !ok || input["command"] != command || payload["tool_name"] != "Bash" || payload["hook_event_name"] != event || payload["cwd"] != f.dir {
						t.Fatalf("unexpected %s fixture payload: %v", event, payload)
					}
					if event == "PostToolUse" {
						response, _ := json.Marshal(payload["tool_response"])
						if !strings.Contains(string(response), "bridge-tool-fixture") {
							t.Fatalf("tool did not complete with expected output: %s", response)
						}
					}
				}
			})
		}
	}
}
