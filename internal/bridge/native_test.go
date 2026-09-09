package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Native tests are deliberately opt-in: a default unit-test run must not start
// installed third-party CLIs. No prompts, logins, installs or approvals occur.
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
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = f.dir
	// Build an allowlisted child environment; never inherit tokens, hooks, auth
	// helpers, or the user's config. These are actual child configuration roots.
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + f.path("home"), "CLAUDE_CONFIG_DIR=" + f.path("claude-home"), "CODEX_HOME=" + f.path("codex-home"), "TMPDIR=" + f.path("tmp"), "NO_COLOR=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native %s %v failed: %v\n%s", filepath.Base(binary), args, err, output)
	}
	return string(output)
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
