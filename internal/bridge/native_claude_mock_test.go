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

func TestNativeClaudeAgentAndHookWithLocalEndpoint(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources = []resourceInput{
		{ID: "skill", Kind: "skill-directory", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/skills/bridge-demo", Codex: "home/.agents/skills/bridge-demo"},
		{ID: "agent", Kind: "agent-file", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/agents/reviewer.md", Codex: "codex-home/agents/reviewer.toml"},
		{ID: "startup", Kind: "hook-config", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/settings.json", Codex: "codex-home/hooks.json"},
		{ID: "instructions", Kind: "instruction-file", Scope: "global", Portable: true, Claude: "claude-home/CLAUDE.md", Codex: "codex-home/AGENTS.md"},
	}
	f.load()
	f.write("home/.agents/skills/bridge-demo/SKILL.md", "---\nname: bridge-demo\ndescription: BRIDGE_STRICT_SKILL_DESCRIPTION_FIXTURE\n---\nHarmless instruction.\n")
	f.write("codex-home/agents/reviewer.toml", "name='reviewer'\ndescription='Review fixture'\ndeveloper_instructions='BRIDGE_AGENT_INSTRUCTION_FIXTURE'\n")
	f.write("codex-home/AGENTS.md", instructionStart+"\nBRIDGE_SHARED_INSTRUCTION_FIXTURE\n"+instructionEnd+"\n")
	f.write("capture-hook", "#!/bin/sh\nexec /bin/cat > '"+f.path("payload.json")+"'\n")
	must(t, os.Chmod(f.path("capture-hook"), 0700))
	data, err := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"matcher": "^startup$", "hooks": []any{map[string]any{"type": "command", "command": f.path("capture-hook"), "timeout": 10}}}}}})
	must(t, err)
	f.write("codex-home/hooks.json", string(data))
	f.apply()
	requests := make(chan string, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/count_tokens") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"input_tokens":10}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		requests <- string(body)
		var request struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &request)
		w.Header().Set("Content-Type", "text/event-stream")
		events := []any{
			map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "content": []any{}, "model": request.Model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}},
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "fixture complete"}},
			map[string]any{"type": "content_block_stop", "index": 0},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 2}},
			map[string]string{"type": "message_stop"},
		}
		for _, event := range events {
			data, _ := json.Marshal(event)
			var envelope struct {
				Type string `json:"type"`
			}
			json.Unmarshal(data, &envelope)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", envelope.Type, data)
		}
	}))
	defer server.Close()
	output := nativeRunEnvironment(t, f, tools["claude"], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--agent", "reviewer", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "Return fixture complete.")
	if !strings.Contains(output, "fixture complete") {
		t.Fatal("local fake response not returned")
	}
	var payload map[string]any
	must(t, json.Unmarshal([]byte(f.read("payload.json")), &payload))
	if payload["hook_event_name"] != "SessionStart" || payload["source"] != "startup" || payload["cwd"] != f.dir {
		t.Fatalf("unexpected startup payload: %v", payload)
	}
	found := false
	for len(requests) > 0 {
		body := <-requests
		if strings.Contains(body, "BRIDGE_AGENT_INSTRUCTION_FIXTURE") && strings.Contains(body, "BRIDGE_SHARED_INSTRUCTION_FIXTURE") && strings.Contains(body, "BRIDGE_STRICT_SKILL_DESCRIPTION_FIXTURE") {
			found = true
		}
	}
	if !found {
		t.Fatal("translated agent or shared instructions missing from local request")
	}
}
