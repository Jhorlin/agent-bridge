package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNativeCodexStartupHookTrustAndExecution(t *testing.T) {
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		t.Run(event, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			f.raw.Resources = []resourceInput{{ID: "startup", Kind: "hook-config", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/settings.json", Codex: "codex-home/hooks.json"}}
			f.raw.Resources = append(f.raw.Resources,
				resourceInput{ID: "instructions", Kind: "instruction-file", Scope: "global", Portable: true, Claude: "claude-home/CLAUDE.md", Codex: "codex-home/AGENTS.md"},
				resourceInput{ID: "skill", Kind: "skill-directory", Scope: "global", Portable: true, Claude: "claude-home/skills/bridge-demo", Codex: "home/.agents/skills/bridge-demo"},
				resourceInput{ID: "agent", Kind: "agent-file", Scope: "global", Portable: true, AllowReformat: true, PreserveAgentSettings: true, Claude: "claude-home/agents/reviewer.md", Codex: "codex-home/agents/reviewer.toml"})
			f.load()
			f.write("claude-home/CLAUDE.md", instructionStart+"\nBRIDGE_SHARED_INSTRUCTION_FIXTURE\n"+instructionEnd+"\n")
			f.write("claude-home/skills/bridge-demo/SKILL.md", "---\nname: bridge-demo\ndescription: BRIDGE_SKILL_DESCRIPTION_FIXTURE\n---\nThis is a harmless fixture.")
			f.write("claude-home/agents/reviewer.md", "---\nname: reviewer\ndescription: BRIDGE_AGENT_DESCRIPTION_FIXTURE\n---\nReview the harmless fixture.")
			// Reviewed fixture only: copy the hook's stdin to one disposable file.
			f.write("capture-hook", "#!/bin/sh\nexec /bin/cat > '"+f.path("payload.json")+"'\n")
			must(t, os.Chmod(f.path("capture-hook"), 0700))
			group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": f.path("capture-hook"), "timeout": 10}}}
			if event == "SessionStart" {
				group["matcher"] = "^startup$"
			}
			data, err := json.Marshal(map[string]any{"hooks": map[string]any{event: []any{group}}})
			must(t, err)
			f.write("claude-home/settings.json", string(data))
			f.apply()
			f.write("codex-home/agents/reviewer.toml", f.read("codex-home/agents/reviewer.toml")+"\nmodel_reasoning_effort = 'low'\nsandbox_mode = 'read-only'\n")
			f.write("claude-home/agents/reviewer.md", strings.Replace(f.read("claude-home/agents/reviewer.md"), "Review the harmless fixture.", "Review the updated harmless fixture.", 1))
			f.apply()
			requests := make(chan string, 16)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/responses") {
					http.NotFound(w, r)
					return
				}
				body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
				requests <- string(body)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
				fmt.Fprint(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"fixture-message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\",\"annotations\":[]}]}}\n\n")
				fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
			}))
			defer server.Close()
			f.write("codex-home/config.toml", fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\n[features]\nmulti_agent=true\nplugins=false\n[model_providers.fixture]\nname='Local test fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\nrequest_max_retries=0\nstream_max_retries=0\n", server.URL))
			nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
			f.missing("payload.json")
			nativeRun(t, f, tools["codex"], "exec", "--dangerously-bypass-hook-trust", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
			var fields map[string]any
			must(t, json.Unmarshal([]byte(f.read("payload.json")), &fields))
			if event == "Stop" {
				if _, ok := fields["stop_hook_active"].(bool); !ok {
					t.Fatal("missing stop recursion flag")
				}
			}
			if fields["hook_event_name"] != event || fields["cwd"] != f.dir || (event == "SessionStart" && fields["source"] != "startup") || (event == "UserPromptSubmit" && fields["prompt"] != "Return fixture complete.") {
				t.Fatalf("unexpected startup fields: %v", fields)
			}
			if len(requests) != 2 {
				t.Fatalf("expected two local fake responses, got %d", len(requests))
			}
			for i := 0; i < 2; i++ {
				body := <-requests
				if !strings.Contains(body, "BRIDGE_SHARED_INSTRUCTION_FIXTURE") || !strings.Contains(body, "BRIDGE_SKILL_DESCRIPTION_FIXTURE") {
					t.Fatal("translated instructions or skill metadata not present in local request")
				}
				if !strings.Contains(body, "BRIDGE_AGENT_DESCRIPTION_FIXTURE") {
					t.Fatal("translated agent not advertised in local request")
				}
			}
		})
	}
}
