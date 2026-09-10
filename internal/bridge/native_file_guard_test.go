package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/hookguard"
)

func TestNativeFileGuardHelper(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_NATIVE_GUARD") != "1" {
		t.Skip("subprocess helper")
	}
	os.Exit(hookguard.Run(context.Background(), os.Getenv("AGENT_BRIDGE_GUARD_PROJECT"), os.Getenv("AGENT_BRIDGE_GUARD_SCRIPT"), os.Stdin, os.Stdout))
}

func TestNativeCodexFileGuard(t *testing.T) {
	for _, mode := range []string{"allow", "deny"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			f.write("policy", "#!/bin/sh\ninput=$(/bin/cat)\ncase \"$input\" in *protected.txt*) printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"BRIDGE_NATIVE_FILE_DENIED\"}}';; esac\n")
			must(t, os.Chmod(f.path("policy"), 0700))
			f.write("wrapper", "#!/bin/sh\nexport AGENT_BRIDGE_NATIVE_GUARD=1\nexport AGENT_BRIDGE_GUARD_PROJECT='"+f.dir+"'\nexport AGENT_BRIDGE_GUARD_SCRIPT='"+f.path("policy")+"'\nexec '"+os.Args[0]+"' -test.run='^TestNativeFileGuardHelper$'\n")
			must(t, os.Chmod(f.path("wrapper"), 0700))
			hooks := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "^apply_patch$", "hooks": []any{map[string]any{"type": "command", "command": f.path("wrapper"), "timeout": 15}}}}}}
			data, err := json.Marshal(hooks)
			must(t, err)
			f.write("codex-home/hooks.json", string(data))
			file := "allowed.txt"
			if mode == "deny" {
				file = "protected.txt"
			}
			patch := "*** Begin Patch\n*** Add File: " + file + "\n+fixture\n*** End Patch"
			var calls atomic.Int32
			var denied atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
				if strings.Contains(string(body), "BRIDGE_NATIVE_FILE_DENIED") {
					denied.Store(true)
				}
				n := calls.Add(1)
				if n > 2 {
					http.Error(w, "fixture call cap", 429)
					return
				}
				var item any = map[string]any{"type": "message", "id": "msg", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture complete", "annotations": []any{}}}}
				if n == 1 {
					var request struct {
						Tools []struct {
							Type string `json:"type"`
							Name string `json:"name"`
						} `json:"tools"`
					}
					json.Unmarshal(body, &request)
					custom := false
					for _, tool := range request.Tools {
						if tool.Name == "apply_patch" && tool.Type == "custom" {
							custom = true
						}
					}
					if custom {
						item = map[string]any{"type": "custom_tool_call", "id": "tool", "call_id": "fixture-patch", "name": "apply_patch", "input": patch}
					} else {
						arg, _ := json.Marshal(map[string]string{"input": patch})
						item = map[string]any{"type": "function_call", "id": "tool", "call_id": "fixture-patch", "name": "apply_patch", "arguments": string(arg)}
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []map[string]any{{"type": "response.created", "response": map[string]any{"id": "fixture"}}, {"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": map[string]any{"id": "fixture", "status": "completed", "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}}} {
					data, _ := json.Marshal(event)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
				}
			}))
			defer server.Close()
			f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
			// A known tool-capable model profile selects the native patch tool;
			// the provider is still our loopback fixture, never a real model.
			output := nativeRun(t, f, tools["codex"], "exec", "--model", "gpt-5.5", "--skip-git-repo-check", "--ephemeral", "--sandbox", "workspace-write", "--dangerously-bypass-hook-trust", "Apply the fixed fixture patch.")
			t.Log(output)
			if mode == "allow" {
				f.expect(file, "fixture\n")
			} else {
				f.missing(file)
				if !denied.Load() {
					t.Fatal("denial not delivered")
				}
			}
		})
	}
}
