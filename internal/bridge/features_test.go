package bridge

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func mcpFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "tools", Kind: "mcp-config", Scope: "project", Claude: "claude.json", Codex: "codex.toml", Servers: []string{"docs"}, AllowReformat: true}}
	f.load()
	f.write("claude.json", `{"mcpServers":{"docs":{"command":"demo-server","args":["--safe"],"env":{"DEMO_TOKEN":"${DEMO_TOKEN}"}}}}`)
	return f
}
func TestMCPStdioRoundTrip(t *testing.T) {
	f := mcpFixture(t)
	f.apply()
	out := f.read("codex.toml")
	if !strings.Contains(out, "env_vars") || strings.Contains(out, "${") {
		t.Fatal("environment forwarding not translated")
	}
	f.write("codex.toml", strings.Replace(out, "demo-server", "new-server", 1))
	f.apply()
	if !strings.Contains(f.read("claude.json"), "new-server") || !strings.Contains(f.read("claude.json"), "${DEMO_TOKEN}") {
		t.Fatal("reverse translation failed")
	}
	for _, s := range f.plan().Summaries() {
		if s.Status != "in-sync" {
			t.Fatal(s)
		}
	}
}
func TestMCPHTTPBearerAndHeaderRefs(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", `{"mcpServers":{"docs":{"type":"http","url":"https://example.test/mcp","headers":{"Authorization":"Bearer ${TOKEN}","X-Tenant":"${TENANT}"}}}}`)
	t.Setenv("TOKEN", "never-copy-this-value")
	f.apply()
	output := f.read("codex.toml")
	if !strings.Contains(output, "bearer_token_env_var") || !strings.Contains(output, "env_http_headers") || strings.Contains(output, "never-copy") {
		t.Fatal("bad credential translation")
	}
	f.write("codex.toml", strings.Replace(output, "example.test", "new.example.test", 1))
	f.apply()
	if !strings.Contains(f.read("claude.json"), "Bearer ${TOKEN}") {
		t.Fatal("reference lost")
	}
}
func TestMCPPreservesUnrelatedSettingsAndEntries(t *testing.T) {
	f := mcpFixture(t)
	f.write("codex.toml", "model = 'unchanged'\napproval_policy = 'on-request'\n[mcp_servers.other]\ncommand = 'keep-me'\n[projects.'/example']\ntrust_level = 'untrusted'\n")
	f.apply()
	doc, err := document("codex", mustSnapshot(t, f.path("codex.toml")))
	must(t, err)
	if doc["model"] != "unchanged" || doc["approval_policy"] != "on-request" {
		t.Fatal("unrelated settings lost")
	}
	if doc["mcp_servers"].(map[string]any)["other"].(map[string]any)["command"] != "keep-me" {
		t.Fatal("unselected MCP changed")
	}
	f.write("claude.json", strings.Replace(f.read("claude.json"), `"mcpServers"`, `"largeInteger":9007199254740993,"mcpServers"`, 1))
	f.write("codex.toml", strings.Replace(f.read("codex.toml"), "demo-server", "different-server", 1))
	f.apply()
	if !strings.Contains(f.read("claude.json"), "9007199254740993") {
		t.Fatal("JSON integer precision lost")
	}
}
func mustSnapshot(t *testing.T, file string) *Snapshot {
	t.Helper()
	s, err := snapshot(file)
	must(t, err)
	return s
}
func TestMCPSemanticFormattingIsNotDrift(t *testing.T) {
	f := mcpFixture(t)
	f.apply()
	before := f.read("state/manifest.json")
	f.write("codex.toml", "# user comment\n"+f.read("codex.toml"))
	f.apply()
	f.expect("state/manifest.json", before)
	if !strings.HasPrefix(f.read("codex.toml"), "# user comment") {
		t.Fatal("no-op rewrote TOML")
	}
}
func TestMCPSameSemanticInitialization(t *testing.T) {
	f := mcpFixture(t)
	f.write("codex.toml", "[mcp_servers.docs]\ncommand='demo-server'\nargs=['--safe']\nenv_vars=['DEMO_TOKEN']\n")
	if f.plan().HasConflicts() {
		t.Fatal("equivalent native configurations conflicted")
	}
	f.apply()
}
func TestMCPConflictsAndDeletionProtection(t *testing.T) {
	f := mcpFixture(t)
	f.apply()
	f.write("claude.json", strings.Replace(f.read("claude.json"), "demo-server", "left", 1))
	f.write("codex.toml", strings.Replace(f.read("codex.toml"), "demo-server", "right", 1))
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.write("claude.json", `{"mcpServers":{}}`)
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
}
func TestMCPUnsupportedFieldsNeverLeakValues(t *testing.T) {
	cases := []string{
		`{"command":"demo","env":{"TOKEN":"sensitive-value"}}`,
		`{"type":"sse","url":"https://example.test/mcp"}`,
		`{"type":"http","url":"https://example.test/mcp?token=sensitive-value"}`,
		`{"type":"http","url":"https://user:sensitive-value@example.test/mcp"}`,
		`{"type":"http","url":"https://example.test/mcp","headers":{"Authorization":"Bearer sensitive-value"}}`,
		`{"command":"${EXECUTABLE}"}`,
		`{"command":"demo","env":{"A":"${B}"}}`,
		`{"command":"demo","unknown":"sensitive-value"}`,
	}
	for _, server := range cases {
		t.Run(server[:12], func(t *testing.T) {
			f := mcpFixture(t)
			f.write("claude.json", `{"mcpServers":{"docs":`+server+`}}`)
			_, err := Apply(f.c, Options{})
			if err == nil || strings.Contains(err.Error(), "sensitive-value") {
				t.Fatalf("unsafe error: %v", err)
			}
			f.missing("codex.toml")
			f.missing("state/backups")
		})
	}
}
func TestMCPCodexPolicyIsNotDropped(t *testing.T) {
	for _, setting := range []string{"enabled=false", "disabled_tools=['write']", "auth='chatgpt'", "http_headers={Authorization='secret'}"} {
		t.Run(setting, func(t *testing.T) {
			f := mcpFixture(t)
			f.write("codex.toml", "[mcp_servers.docs]\ncommand='demo-server'\n"+setting+"\n")
			_, err := Plan(f.c)
			contains(t, err, "unsupported MCP option")
		})
	}
}
func TestMCPMalformedAndDuplicateConfigRejected(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", `{"mcpServers":{},"mcpServers":{}}`)
	_, err := Plan(f.c)
	contains(t, err, "duplicate-key")
	f.write("claude.json", "{}")
	f.write("codex.toml", "secret = 'unterminated")
	_, err = Plan(f.c)
	contains(t, err, "invalid Codex TOML")
	if strings.Contains(err.Error(), "unterminated") {
		t.Fatal("parse context leaked")
	}
}
func TestMCPAllowlistAndReformatConsentRequired(t *testing.T) {
	f := mcpFixture(t)
	f.raw.Resources[0].AllowReformat = false
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "allowReformat")
	f.raw.Resources[0].AllowReformat = true
	f.raw.Resources[0].Servers = nil
	f.save()
	_, err = LoadConfig(f.path("config.json"))
	contains(t, err, "allowlist")
}
func TestMCPPartialAllowlistRejected(t *testing.T) {
	f := mcpFixture(t)
	f.raw.Resources[0].Servers = []string{"docs", "other"}
	f.load()
	_, err := Plan(f.c)
	contains(t, err, "partial MCP allowlist")
}
func TestMCPRollbackRestoresExactOriginalFormatting(t *testing.T) {
	f := mcpFixture(t)
	f.write("codex.toml", "# preserve me\nmodel='x'\n")
	before := f.read("codex.toml")
	_, err := Apply(f.c, Options{BeforeWrite: func(i int, op Operation) error {
		if op.Label == "manifest" {
			return errors.New("stop")
		}
		return nil
	}})
	contains(t, err, "rolled back")
	f.expect("codex.toml", before)
	f.missing("state/shared/tools")
}
func TestMCPMultipleServers(t *testing.T) {
	f := mcpFixture(t)
	f.raw.Resources[0].Servers = []string{"docs", "other"}
	f.load()
	f.write("claude.json", `{"mcpServers":{"docs":{"command":"one"},"other":{"command":"two"}}}`)
	f.apply()
	if !strings.Contains(f.read("codex.toml"), "two") {
		t.Fatal("missing second MCP")
	}
}

func TestInheritanceOverrideResolvesPathsAndPreservesGlobal(t *testing.T) {
	f := newFixture(t)
	f.apply()
	global := f.read("AGENTS.md")
	child := configInput{Version: 1, StateDir: "state", Extends: "../config.json", Resources: []resourceInput{{ID: "rules", Kind: "portable-file", Scope: "project", Claude: "CLAUDE.md", Codex: "AGENTS.md"}}}
	b, _ := json.Marshal(child)
	f.write("project/config.json", string(b))
	f.write("project/CLAUDE.md", "project")
	c, err := LoadConfig(f.path("project/config.json"))
	must(t, err)
	if c.Resources[0].Paths["claude"] != f.path("project/CLAUDE.md") {
		t.Fatal("wrong relative base")
	}
	_, err = Apply(c, Options{})
	must(t, err)
	f.expect("project/AGENTS.md", "project")
	f.expect("AGENTS.md", global)
}
func TestInheritanceUnchangedGlobalPathsAndDisable(t *testing.T) {
	f := newFixture(t)
	child := configInput{Version: 1, StateDir: "state", Extends: "../config.json", Resources: []resourceInput{}}
	b, _ := json.Marshal(child)
	f.write("project/config.json", string(b))
	c, err := LoadConfig(f.path("project/config.json"))
	must(t, err)
	if c.Resources[0].Paths["claude"] != f.path("CLAUDE.md") {
		t.Fatal("inherited path moved")
	}
	child.Disable = []string{"rules"}
	b, _ = json.Marshal(child)
	f.write("project/config.json", string(b))
	c, err = LoadConfig(f.path("project/config.json"))
	must(t, err)
	if len(c.Resources) != 0 {
		t.Fatal("disable ignored")
	}
	f.expect("CLAUDE.md", "one")
}
func TestInheritanceCycleAndBadDisable(t *testing.T) {
	f := newFixture(t)
	f.raw.Extends = "config.json"
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "cycle")
	f.raw.Extends = ""
	f.raw.Disable = []string{"missing"}
	f.save()
	_, err = LoadConfig(f.path("config.json"))
	contains(t, err, "does not exist")
}
func TestInheritanceNoImplicitFieldMerge(t *testing.T) {
	f := newFixture(t)
	child := configInput{Version: 1, StateDir: "state", Extends: "../config.json", Resources: []resourceInput{{ID: "rules", Scope: "project", Claude: "CLAUDE.md", Codex: "AGENTS.md"}}}
	b, _ := json.Marshal(child)
	f.write("project/config.json", string(b))
	_, err := LoadConfig(f.path("project/config.json"))
	contains(t, err, "unsupported adapter")
}

func TestPinnedFileSymlinkPreservedBidirectionally(t *testing.T) {
	f := newFixture(t)
	must(t, os.Rename(f.path("CLAUDE.md"), f.path("canonical.md")))
	must(t, os.Symlink("canonical.md", f.path("CLAUDE.md")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "canonical.md"}
	f.load()
	f.apply()
	f.write("AGENTS.md", "reverse")
	f.apply()
	f.expect("canonical.md", "reverse")
	link, err := os.Readlink(f.path("CLAUDE.md"))
	must(t, err)
	if link != "canonical.md" {
		t.Fatal("symlink replaced")
	}
}
func TestPinnedSkillDirectorySymlink(t *testing.T) {
	f := skillFixture(t)
	must(t, os.Rename(f.path("claude-skill"), f.path("skill-source")))
	must(t, os.Symlink("skill-source", f.path("claude-skill")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "skill-source"}
	f.load()
	f.apply()
	f.expect("codex-skill/SKILL.md", f.read("skill-source/SKILL.md"))
}
func TestPinnedSymlinkRetargetBlocksWrites(t *testing.T) {
	f := newFixture(t)
	must(t, os.Rename(f.path("CLAUDE.md"), f.path("canonical.md")))
	must(t, os.Symlink("canonical.md", f.path("CLAUDE.md")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "canonical.md"}
	f.load()
	f.write("other.md", "do not touch")
	must(t, os.Remove(f.path("CLAUDE.md")))
	must(t, os.Symlink("other.md", f.path("CLAUDE.md")))
	_, err := Apply(f.c, Options{})
	contains(t, err, "target changed")
	f.missing("AGENTS.md")
	f.expect("other.md", "do not touch")
}
func TestPinnedMissingTargetAndAliasCollision(t *testing.T) {
	f := newFixture(t)
	must(t, os.Remove(f.path("CLAUDE.md")))
	must(t, os.Symlink("missing.md", f.path("CLAUDE.md")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "missing.md"}
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "missing")
	f.write("missing.md", "one")
	f.raw.Resources[0].Codex = "missing.md"
	f.save()
	_, err = LoadConfig(f.path("config.json"))
	contains(t, err, "overlap")
}

func pluginFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "bundle", Kind: "plugin-directory", Portable: true, Scope: "project", Claude: "claude-plugin", Codex: "codex-plugin"}}
	f.load()
	f.write("claude-plugin/.claude-plugin/plugin.json", `{"name":"demo","version":"1.0.0","description":"Portable demo"}`)
	f.write("claude-plugin/skills/demo/SKILL.md", "---\nname: demo\ndescription: Portable demo.\n---\nRead the docs.")
	return f
}
func TestPluginPackageTranslationAndReverseEdit(t *testing.T) {
	f := pluginFixture(t)
	f.apply()
	if !strings.Contains(f.read("codex-plugin/.codex-plugin/plugin.json"), "./skills/") {
		t.Fatal("Codex skills declaration missing")
	}
	f.expect("codex-plugin/skills/demo/SKILL.md", f.read("claude-plugin/skills/demo/SKILL.md"))
	f.write("codex-plugin/.codex-plugin/plugin.json", strings.Replace(f.read("codex-plugin/.codex-plugin/plugin.json"), "1.0.0", "1.1.0", 1))
	f.write("codex-plugin/skills/demo/SKILL.md", "edited on codex")
	f.apply()
	if !strings.Contains(f.read("claude-plugin/.claude-plugin/plugin.json"), "1.1.0") {
		t.Fatal("metadata did not sync back")
	}
	f.expect("claude-plugin/skills/demo/SKILL.md", "edited on codex")
}
func TestPluginUnsupportedComponentsBlockAllWrites(t *testing.T) {
	for _, file := range []string{"hooks/hooks.json", ".mcp.json", ".app.json", "agents/reviewer.md", "settings.json", "plugin.json"} {
		t.Run(file, func(t *testing.T) {
			f := pluginFixture(t)
			f.write("claude-plugin/"+file, "{}")
			_, err := Apply(f.c, Options{})
			contains(t, err, "unsupported component")
			f.missing("codex-plugin")
		})
	}
}
func TestPluginUnsupportedManifestAndCustomPaths(t *testing.T) {
	for _, extra := range []string{`,"hooks":{}`, `,"skills":"./custom/"`, `,"interface":{}`, `,"mcpServers":"./.mcp.json"`} {
		t.Run(extra, func(t *testing.T) {
			f := pluginFixture(t)
			f.write("claude-plugin/.claude-plugin/plugin.json", `{"name":"demo"`+extra+`}`)
			_, err := Apply(f.c, Options{})
			if err == nil {
				t.Fatal("unsupported manifest accepted")
			}
			f.missing("codex-plugin")
		})
	}
}
func TestPluginManifestConflict(t *testing.T) {
	f := pluginFixture(t)
	f.apply()
	f.write("claude-plugin/.claude-plugin/plugin.json", `{"name":"demo","version":"left"}`)
	f.write("codex-plugin/.codex-plugin/plugin.json", `{"name":"demo","version":"right"}`)
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
}
func TestPluginFailureRecoveryAndNoInstallSideEffects(t *testing.T) {
	f := pluginFixture(t)
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.missing("codex-plugin/.codex-plugin/plugin.json")
	f.missing("marketplace.json")
	f.apply()
}

func TestMCPPropertyRoundTrips(t *testing.T) {
	servers := []MCPServer{
		{Transport: "stdio", Command: "server"},
		{Transport: "stdio", Command: "server", Args: []string{"--path", "space and unicode 文"}, EnvVars: []string{"Z_VAR", "A_VAR"}, CWD: "/example/work"},
		{Transport: "http", URL: "https://example.test/mcp", BearerEnv: "TOKEN"},
		{Transport: "http", URL: "http://localhost:9000/mcp", HeaderVars: map[string]string{"X-Token": "TOKEN", "X-Tenant": "TENANT"}},
	}
	r := Resource{ID: "roundtrip", Servers: []string{"docs"}}
	for _, s := range servers {
		canonical, err := canonicalServer(s)
		must(t, err)
		content, err := encoded(map[string]MCPServer{"docs": canonical})
		must(t, err)
		for _, side := range sides {
			rendered, err := renderMCP(r, side, content, nil)
			must(t, err)
			got, err := normalizeMCP(r, side, rendered)
			must(t, err)
			if fingerprint(got) != fingerprint(content) {
				t.Fatalf("round-trip differs for %s", side)
			}
		}
	}
}

func TestPluginPortableCodexLayout(t *testing.T) {
	f := pluginFixture(t)
	f.raw.Resources[0].CodexPluginLayout = "portable"
	f.load()
	f.apply()
	output := f.read("codex-plugin/plugin.json")
	if !strings.Contains(output, portablePluginSchema) {
		t.Fatal("portable schema missing")
	}
	f.missing("codex-plugin/.codex-plugin/plugin.json")
	f.write("codex-plugin/plugin.json", strings.Replace(output, `"1.0.0"`, `"2.0.0"`, 1))
	f.apply()
	if !strings.Contains(f.read("claude-plugin/.claude-plugin/plugin.json"), "2.0.0") {
		t.Fatal("portable reverse translation failed")
	}
}

func TestMCPUnselectedUnsafeEntryIsPreservedNotExported(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", `{"mcpServers":{"docs":{"command":"demo-server"},"private":{"command":"private","env":{"TOKEN":"private-secret"}}}}`)
	f.apply()
	if strings.Contains(f.read("state/shared/tools"), "private-secret") || strings.Contains(f.read("codex.toml"), "private") {
		t.Fatal("unselected entry exported")
	}
	if !strings.Contains(f.read("claude.json"), "private-secret") {
		t.Fatal("unselected entry changed")
	}
}

func TestPinnedRetargetDuringTransactionRollsBackOriginal(t *testing.T) {
	f := newFixture(t)
	must(t, os.Rename(f.path("CLAUDE.md"), f.path("canonical.md")))
	must(t, os.Symlink("canonical.md", f.path("CLAUDE.md")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "canonical.md"}
	f.load()
	f.write("elsewhere.md", "keep")
	_, err := Apply(f.c, Options{BeforeWrite: func(i int, _ Operation) error {
		if i == 1 {
			must(t, os.Remove(f.path("CLAUDE.md")))
			must(t, os.Symlink("elsewhere.md", f.path("CLAUDE.md")))
		}
		return nil
	}})
	contains(t, err, "rolled back")
	f.expect("canonical.md", "one")
	f.expect("elsewhere.md", "keep")
	f.missing("AGENTS.md")
}

func TestDuplicateInheritanceKeysRejected(t *testing.T) {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"one","stateDir":"two","resources":[]}`)
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "duplicate-key")
}

func TestMixedResourcesConflictIsAllOrNothing(t *testing.T) {
	f := mcpFixture(t)
	f.raw.Resources = append(f.raw.Resources, resourceInput{ID: "rules", Kind: "portable-file", Scope: "global", Claude: "CLAUDE.md", Codex: "AGENTS.md"})
	f.load()
	f.write("AGENTS.md", "conflict")
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.missing("codex.toml")
	f.missing("state/shared/tools")
}

func FuzzMCPJSON(f *testing.F) {
	for _, seed := range []string{`{}`, `null`, `{"mcpServers":{"docs":{"command":"server"}}}`, `{"mcpServers":{"docs":{"headers":[]}}}`, `{"mcpServers":{},"mcpServers":{}}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		_, _ = normalizeMCP(Resource{ID: "fuzz", Servers: []string{"docs"}}, "claude", raw)
	})
}
