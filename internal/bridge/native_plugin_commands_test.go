package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Verify the generated authoring package, not Codex's private migrated cache.
func TestNativePluginCommandDiscovery(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	f.load()
	f.write("claude-plugin/commands/bridge-command.md", "---\ndescription: Bridge command discovery marker.\n---\nReturn the fixed word fixture.\n")
	f.apply()
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	nativeRPCSession(t, f, tools["codex"], func(call func(string, any) json.RawMessage) {
		call("plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
		skills := call("skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
		if !strings.Contains(string(skills), "Bridge command discovery marker") || !strings.Contains(string(skills), "demo:source-command-bridge-command") {
			t.Fatal("generated command not discovered as a migrated plugin skill")
		}
		call("plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
	})
	// Reverse authoring changes survive Claude's installer and validation.
	f.write("home/plugins/demo/commands/bridge-command.md", "---\ndescription: Reverse command marker.\n---\nReturn the fixed word fixture.\n")
	f.apply()
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "add", f.dir)
	nativeRun(t, f, tools["claude"], "plugin", "install", "demo@bridge-fixture", "--scope", "user")
	nativeRun(t, f, tools["claude"], "plugin", "validate", f.path("claude-plugin"))
	requests := make(chan string, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			select {
			case requests <- string(body):
			default:
				http.Error(w, "fixture request cap", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
			fmt.Fprint(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"fixture-message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\",\"annotations\":[]}]}}\n\n")
			fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
			return
		}
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
		select {
		case requests <- string(body):
		default:
			http.Error(w, "fixture request cap", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","content":[],"model":"fixture","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fixture complete"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		}
		for _, event := range events {
			var envelope struct{ Type string }
			json.Unmarshal([]byte(event), &envelope)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", envelope.Type, event)
		}
	}))
	defer server.Close()
	nativeRunEnvironment(t, f, tools["claude"], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "/demo:bridge-command")
	if len(requests) == 0 {
		t.Fatal("no local command invocation request")
	}
	found := false
	for len(requests) > 0 {
		found = strings.Contains(<-requests, "Return the fixed word fixture.") || found
	}
	if !found {
		t.Fatal("Claude did not expand the reverse-generated command body")
	}
	f.write("codex-home/config.toml", fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\n[model_providers.fixture]\nname='Local test fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\nrequest_max_retries=0\nstream_max_retries=0\n", server.URL))
	nativeRPC(t, f, tools["codex"], "plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
	nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "$demo:source-command-bridge-command")
	found = false
	for len(requests) > 0 {
		found = strings.Contains(<-requests, "Return the fixed word fixture.") || found
	}
	if !found {
		t.Fatal("Codex did not expand the installed migrated command body")
	}
	nativeRPC(t, f, tools["codex"], "plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
	nativeRun(t, f, tools["claude"], "plugin", "uninstall", "demo@bridge-fixture", "--scope", "user")
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "remove", "bridge-fixture")
	if !strings.Contains(f.read("claude-plugin/commands/bridge-command.md"), "Reverse command marker") {
		t.Fatal("reverse command update missing")
	}
}
