package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Verify defaults exposed by native metadata and acceptance of explicit native
// defaults without waiting for a long timeout. Claude exposes no equivalent
// timeout metadata: this checks execution, not its elapsed timeout duration.
func TestNativeHookDefaultMetadataAndExplicitTimeouts(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			hooks := map[string]any{}
			wantTimeouts := map[string]int{}
			for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse"} {
				groups := []any{}
				for _, seconds := range []int{0, 30, 600} {
					label := event + "-" + strconv.Itoa(seconds)
					nativeHookDefaultCaptureScript(t, f, label)
					handler := map[string]any{"type": "command", "command": f.path(label)}
					wantTimeouts[label] = 600
					if seconds != 0 {
						handler["timeout"] = seconds
						wantTimeouts[label] = seconds
					}
					group := map[string]any{"hooks": []any{handler}}
					if event == "SessionStart" {
						group["matcher"] = "^startup$"
					} else if event == "PreToolUse" || event == "PostToolUse" {
						group["matcher"] = "^Bash$"
					}
					groups = append(groups, group)
				}
				hooks[event] = groups
			}
			raw, err := json.Marshal(map[string]any{"hooks": hooks})
			must(t, err)
			server, _ := nativePluginFixtureProvider(t)
			if host == "claude" {
				f.write("claude-home/settings.json", string(raw))
				nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "Return fixture complete.")
			} else {
				f.write("codex-home/hooks.json", string(raw))
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL))
				metadata := nativeRPC(t, f, tools[host], "hooks/list", map[string]any{"cwds": []string{f.dir}})
				var doc struct {
					Data []struct {
						Hooks []struct {
							Command, HandlerType, TrustStatus string
							TimeoutSec                        int
						}
						Errors []any
					}
				}
				must(t, json.Unmarshal(metadata, &doc))
				seen := map[string]bool{}
				for _, data := range doc.Data {
					if len(data.Errors) != 0 {
						t.Fatal("native hook discovery reported errors")
					}
					for _, hook := range data.Hooks {
						label := filepath.Base(hook.Command)
						want, ok := wantTimeouts[label]
						if !ok || hook.TimeoutSec != want || hook.HandlerType != "command" || hook.TrustStatus != "untrusted" {
							t.Fatalf("unexpected native hook metadata: %s timeout=%d", label, hook.TimeoutSec)
						}
						seen[label] = true
					}
				}
				if len(seen) != len(wantTimeouts) {
					t.Fatal("native hook inventory omitted a timeout fixture")
				}
				// Trust only these reviewed inert captures in this child invocation.
				nativeRun(t, f, tools[host], "exec", "--dangerously-bypass-hook-trust", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
			}
			for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
				for _, seconds := range []int{0, 30, 600} {
					var payload struct {
						Event  string `json:"hook_event_name"`
						Source string `json:"source"`
					}
					must(t, json.Unmarshal([]byte(f.read(event+"-"+strconv.Itoa(seconds)+".json")), &payload))
					if payload.Event != event || (event == "SessionStart" && payload.Source != "startup") {
						t.Fatal("native hook did not execute with expected event")
					}
				}
			}
		})
	}
}

// Claude's unqualified SessionStart hook also runs on fork. Reducing that group
// to startup/resume/clear/compact would silently narrow its native semantics.
func TestNativeClaudeSessionStartForkSource(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	groups := []any{}
	for _, spec := range []struct{ name, matcher string }{{"all", ""}, {"common", "^(startup|resume|clear|compact)$"}, {"fork", "^fork$"}} {
		nativeHookDefaultCaptureScript(t, f, spec.name)
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": f.path(spec.name), "timeout": 600}}}
		if spec.matcher != "" {
			group["matcher"] = spec.matcher
		}
		groups = append(groups, group)
	}
	raw, err := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": groups}})
	must(t, err)
	f.write("claude-home/settings.json", string(raw))
	server, _ := nativePluginFixtureProvider(t)
	env := []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}
	const sessionID = "ba628773-6247-4ac0-b86f-762bb8084810"
	// Persistence is enabled only inside this disposable home, to fork this
	// fixture's own saved response. No user conversation is read or modified.
	nativeRunEnvironment(t, f, tools["claude"], env, "--print", "--model", "sonnet", "--max-turns", "1", "--setting-sources", "user", "--session-id", sessionID, "Return fixture complete.")
	f.missing("fork.json")
	commonBefore := f.read("common.json")
	nativeRunEnvironment(t, f, tools["claude"], env, "--print", "--model", "sonnet", "--max-turns", "1", "--setting-sources", "user", "--resume", sessionID, "--fork-session", "Return fixture complete.")
	for _, name := range []string{"all", "fork"} {
		var payload struct{ Source string }
		must(t, json.Unmarshal([]byte(f.read(name+".json")), &payload))
		if payload.Source != "fork" {
			t.Fatal("native SessionStart fork source changed")
		}
	}
	if f.read("common.json") != commonBefore {
		t.Fatal("common-source matcher unexpectedly ran on fork")
	}
}

func nativeHookDefaultCaptureScript(t *testing.T, f *fixture, name string) {
	t.Helper()
	f.write(name, "#!/bin/sh\nexec /bin/cat > '"+f.path(name+".json")+"'\n")
	must(t, os.Chmod(f.path(name), 0700))
}
