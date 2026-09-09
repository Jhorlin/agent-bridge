package bridge

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Native tests are deliberately opt-in: a default unit-test run must not start
// installed third-party CLIs. No real-provider requests, logins or production
// approvals occur. Fixed prompts use loopback fake providers only; runtime tests
// approve only reviewed fixtures in disposable test configuration.
func nativeTools(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	if os.Getenv("AGENT_BRIDGE_NATIVE_TESTS") != "1" {
		t.Skip("set AGENT_BRIDGE_NATIVE_TESTS=1 for isolated native CLI acceptance tests")
	}
	result := map[string]string{}
	for _, name := range []string{"claude", "codex"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Skipf("%s CLI unavailable", name)
		}
		result[name] = path
	}
	for _, dir := range []string{"home", "claude-home", "codex-home", "tmp"} {
		must(t, os.MkdirAll(f.path(dir), 0700))
	}
	for _, name := range []string{"claude", "codex"} {
		t.Logf("%s version: %s", name, strings.TrimSpace(nativeRun(t, f, result[name], "--version")))
	}
	return result
}

func nativeRun(t *testing.T, f *fixture, binary string, args ...string) string {
	return nativeRunEnvironment(t, f, binary, nil, args...)
}

func nativeRunEnvironment(t *testing.T, f *fixture, binary string, extra []string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	// A CLI can exit before its background cache helpers do. Isolate and reap
	// this fixture's process group so TempDir cleanup cannot race those helpers.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	defer func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	cmd.Dir = f.dir
	// Build an allowlisted child environment; never inherit tokens, hooks, auth
	// helpers, or the user's config. These are actual child configuration roots.
	cmd.Env = append(nativeEnvironment(f), extra...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native %s %v failed: %v\n%s", filepath.Base(binary), args, err, output)
	}
	return string(output)
}

func nativeEnvironment(f *fixture) []string {
	return []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + f.path("home"), "CLAUDE_CONFIG_DIR=" + f.path("claude-home"), "CODEX_HOME=" + f.path("codex-home"), "TMPDIR=" + f.path("tmp"), "NO_COLOR=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"}
}

func nativeRPC(t *testing.T, f *fixture, binary, method string, params any) json.RawMessage {
	var result json.RawMessage
	nativeRPCSession(t, f, binary, func(call func(string, any) json.RawMessage) { result = call(method, params) })
	return result
}

func nativeRPCSession(t *testing.T, f *fixture, binary string, run func(func(string, any) json.RawMessage)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = f.dir
	cmd.Env = nativeEnvironment(f)
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	must(t, err)
	output, err := cmd.StdoutPipe()
	must(t, err)
	must(t, cmd.Start())
	defer func() { input.Close(); syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() }()
	encoder := json.NewEncoder(input)
	decoder := json.NewDecoder(output)
	must(t, encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "agent_bridge_tests", "version": "0.1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}}))
	read := func(want int) json.RawMessage {
		for {
			var message struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			must(t, decoder.Decode(&message))
			if message.ID == want {
				if len(message.Error) > 0 {
					t.Fatalf("native RPC error: %s", message.Error)
				}
				return message.Result
			}
		}
	}
	read(1)
	must(t, encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}))
	id := 1
	run(func(method string, params any) json.RawMessage {
		id++
		must(t, encoder.Encode(map[string]any{"id": id, "method": method, "params": params}))
		return read(id)
	})
}

func TestNativeCodexSkillAndHookDiscovery(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources = []resourceInput{
		{ID: "skill", Kind: "skill-directory", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/skills/bridge-demo", Codex: "home/.agents/skills/bridge-demo"},
		{ID: "hook", Kind: "hook-config", Scope: "global", Portable: true, AllowReformat: true, Claude: "claude-home/settings.json", Codex: "codex-home/hooks.json"},
	}
	f.load()
	f.write("claude-home/skills/bridge-demo/SKILL.md", "---\nname: bridge-demo\ndescription: Harmless bridge discovery fixture.\n---\nExplain that this is a test.\n")
	f.write("claude-home/settings.json", `{"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"/usr/bin/true","timeout":10}]}]}}`)
	f.apply()
	params := map[string]any{"cwds": []string{f.dir}, "forceReload": true}
	skills := nativeRPC(t, f, tools["codex"], "skills/list", params)
	if !strings.Contains(string(skills), `"name":"bridge-demo"`) {
		t.Fatalf("skill not discovered: %s", skills)
	}
	hooks := nativeRPC(t, f, tools["codex"], "hooks/list", map[string]any{"cwds": []string{f.dir}})
	if !strings.Contains(string(hooks), "/usr/bin/true") || !strings.Contains(string(hooks), `"eventName":"sessionStart"`) || !strings.Contains(string(hooks), `"trustStatus":"untrusted"`) {
		t.Fatalf("hook not discovered: %s", hooks)
	}
	t.Logf("Codex discovered generated skill and hook; no hook trust or execution was requested")
}

func TestNativeClaudeAgentValidation(t *testing.T) {
	f := agentFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Claude = "claude-home/agents/reviewer.md"
	f.raw.Resources[0].Codex = "codex-home/agents/reviewer.toml"
	f.load()
	f.write("claude-home/agents/reviewer.md", f.read("claude-source"))
	f.apply()
	output := nativeRun(t, f, tools["claude"], "plugin", "validate", f.path("claude-home/agents"), "--json")
	if !json.Valid([]byte(output)) {
		t.Fatal("agent validator did not return JSON")
	}
}

func TestNativeMCPConfigurationAcceptance(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources = []resourceInput{{ID: "native-tools", Kind: "mcp-config", Scope: "project", Claude: ".mcp.json", Codex: "codex-home/config.toml", Servers: []string{"bridge-test"}, AllowReformat: true}}
	f.load()
	// An unapproved project server is inspected, never started by Claude get.
	f.write(".mcp.json", `{"mcpServers":{"bridge-test":{"command":"/usr/bin/true","args":[]}}}`)
	f.apply()
	var codex map[string]any
	must(t, json.Unmarshal([]byte(nativeRun(t, f, tools["codex"], "mcp", "get", "bridge-test", "--json")), &codex))
	transport, ok := codex["transport"].(map[string]any)
	if !ok || transport["command"] != "/usr/bin/true" {
		t.Fatalf("Codex did not load translated command: %#v", codex)
	}
	output := nativeRun(t, f, tools["claude"], "mcp", "get", "bridge-test")
	if !strings.Contains(output, "bridge-test:") || !strings.Contains(strings.ToLower(output), "pending approval") {
		t.Fatalf("Claude did not report unapproved fixture: %s", output)
	}
	// A reverse edit must still be accepted by Claude's actual config reader.
	f.write("codex-home/config.toml", strings.Replace(f.read("codex-home/config.toml"), "/usr/bin/true", "/usr/bin/false", 1))
	f.apply()
	output = nativeRun(t, f, tools["claude"], "mcp", "get", "bridge-test")
	if !strings.Contains(output, "bridge-test:") || !strings.Contains(strings.ToLower(output), "pending approval") {
		t.Fatal("Claude did not recognize reverse-edited project server")
	}
	if !strings.Contains(f.read(".mcp.json"), "/usr/bin/false") {
		t.Fatal("reverse edit missing")
	}
}

func TestNativeClaudePortablePackageValidation(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.apply()
	for _, path := range []string{"claude-plugin", "codex-plugin"} {
		// Validate Claude's native package and the shared skill content generated
		// for Codex. This is NOT Codex plugin installation/discovery certification.
		target := f.path(path)
		if path == "codex-plugin" {
			target = filepath.Join(target, "skills")
		}
		output := nativeRun(t, f, tools["claude"], "plugin", "validate", target, "--json")
		if !json.Valid([]byte(output)) {
			t.Fatalf("validation did not return JSON: %s", output)
		}
	}
}
