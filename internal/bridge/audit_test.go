package bridge

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func auditTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	must(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		result[path] = info.Mode().String()
		if d.Type().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] += string(b)
		}
		return nil
	}))
	return result
}
func TestAuditAllAdaptersReadOnlyAndDeterministic(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) *fixture{"file": newFixture, "skill": skillFixture, "mcp": mcpFixture, "plugin": pluginFixture} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			for _, adopt := range []bool{false, true} {
				if adopt {
					f.apply()
				}
				before := auditTree(t, f.dir)
				a := Audit(f.c)
				b := Audit(f.c)
				if a.Blocked() || !a.ReadOnly || a.HostVerified || !reflect.DeepEqual(a, b) {
					t.Fatalf("unexpected report: %+v", a)
				}
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("audit mutated fixture")
				}
			}
		})
	}
}
func TestAuditUnsupportedAndMalformedMCPRedacted(t *testing.T) {
	for _, input := range []string{
		`{"mcpServers":{"docs":{"command":"secret-value","enabled":false,"secret-key":"secret-value"}}}`,
		`{"mcpServers":{"docs":{"command":"demo","env":{"TOKEN":"secret-value"}}}}`,
		`{"mcpServers": {"secret-key": "secret-value"`,
	} {
		f := mcpFixture(t)
		f.write("claude.json", input)
		before := auditTree(t, f.dir)
		a := Audit(f.c)
		if !a.Blocked() {
			t.Fatal("accepted unsupported input")
		}
		b, err := json.Marshal(a)
		must(t, err)
		if strings.Contains(string(b), "secret-") || strings.Contains(string(b), f.dir) {
			t.Fatal("audit leaked input")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("audit wrote files")
		}
	}
}
func TestAuditFieldInventoryAndBothNativeSides(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", `{"mcpServers":{"docs":{"command":"demo","enabled":false,"PRIVATE_KEY":"hidden"}}}`)
	f.write("codex.toml", "[mcp_servers.docs]\ncommand='demo'\ntool_timeout_sec=2\n")
	a := Audit(f.c)
	checks := a.Resources[0].Checks
	if len(checks) != 2 || checks[0].UnknownFields != 1 || !reflect.DeepEqual(checks[0].UnsupportedFields, []string{"enabled"}) || !reflect.DeepEqual(checks[1].UnsupportedFields, []string{"tool_timeout_sec"}) {
		t.Fatalf("bad fields: %+v", checks)
	}
}
func TestAuditPluginUnsupportedComponents(t *testing.T) {
	for _, name := range []string{"hooks/hooks.json", ".mcp.json", "agents/reviewer.md", "commands/demo.md", ".app.json"} {
		f := pluginFixture(t)
		f.write("claude-plugin/"+name, "secret-value")
		before := auditTree(t, f.dir)
		if !Audit(f.c).Blocked() {
			t.Fatalf("accepted %s", name)
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("audit mutated fixture")
		}
	}
}
func TestAuditConflictPendingAndUnsafePaths(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		f := newFixture(t)
		f.write("AGENTS.md", "different")
		if !Audit(f.c).Blocked() {
			t.Fatal("missed conflict")
		}
	})
	t.Run("pending", func(t *testing.T) {
		f := newFixture(t)
		f.apply()
		f.write("state/pending.json", `{"transaction":"interrupted"}`)
		before := auditTree(t, f.dir)
		if !Audit(f.c).Blocked() {
			t.Fatal("missed pending")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("changed state")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		f := newFixture(t)
		must(t, os.Symlink(f.path("CLAUDE.md"), f.path("AGENTS.md")))
		if !Audit(f.c).Blocked() {
			t.Fatal("accepted link")
		}
	})
}
func TestAuditCollectsAllResourcesInIDOrder(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources = append(f.raw.Resources, resourceInput{ID: "aaa", Kind: "portable-file", Scope: "project", Claude: "missing-a", Codex: "missing-b"})
	f.load()
	a := Audit(f.c)
	if len(a.Resources) != 2 || a.Resources[0].ID != "aaa" || a.Resources[0].Status != "blocked" || a.Resources[1].Status != "review-required" {
		t.Fatalf("bad report %+v", a)
	}
}

func TestAuditProfileInheritanceAndStrictFields(t *testing.T) {
	f := newFixture(t)
	f.write("child.json", `{"version":1,"stateDir":"child-state","extends":"config.json","resources":[]}`)
	c, err := LoadAuditConfig(f.path("child.json"))
	must(t, err)
	if len(c.Resources) != 1 || c.Resources[0].Paths["claude"] != f.path("CLAUDE.md") {
		t.Fatal("inherited paths changed")
	}
	if Audit(c).Blocked() {
		t.Fatal("valid inherited profile blocked")
	}
	for _, input := range []string{
		`{"version":1,"stateDir":"state","resources":[],"private-key":"private-value"}`,
		`{"version":1,"stateDir":"state","resources":[{"id":"x","private-key":"private-value"}]}`,
		`{"version":1,"version":1,"stateDir":"state","resources":[]}`,
		`{"version":1,"stateDir":"state","extends":"child.json","resources":[]}`,
		`{"version":1,"stateDir":"state","resources":[]} {}`,
	} {
		f.write("config.json", input)
		before := auditTree(t, f.dir)
		if _, err := LoadAuditConfig(f.path("child.json")); err == nil {
			t.Fatal("invalid parent accepted")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("profile loader wrote files")
		}
	}
}

func TestAuditProfileMissingAndLinked(t *testing.T) {
	f := newFixture(t)
	if _, err := LoadAuditConfig(f.path("missing.json")); err == nil {
		t.Fatal("missing profile accepted")
	}
	must(t, os.Symlink(f.path("config.json"), f.path("linked.json")))
	if _, err := LoadAuditConfig(f.path("linked.json")); err == nil {
		t.Fatal("symlink profile accepted")
	}
	must(t, os.Link(f.path("config.json"), f.path("hardlinked.json")))
	if _, err := LoadAuditConfig(f.path("hardlinked.json")); err == nil {
		t.Fatal("hardlink profile accepted")
	}
}

func TestAuditMalformedAndUnknownPluginManifest(t *testing.T) {
	for _, input := range []string{`{"name":"demo","hooks":{},"PRIVATE_KEY":"PRIVATE_VALUE"}`, `{"name":`, `{"name":"demo","author":{"PRIVATE_KEY":"PRIVATE_VALUE"}}`} {
		f := pluginFixture(t)
		f.write("claude-plugin/.claude-plugin/plugin.json", input)
		a := Audit(f.c)
		if !a.Blocked() {
			t.Fatal("invalid plugin accepted")
		}
		b, err := json.Marshal(a)
		must(t, err)
		if strings.Contains(string(b), "PRIVATE_") {
			t.Fatal("plugin content leaked")
		}
	}
	f := pluginFixture(t)
	must(t, os.Symlink(f.path("claude-plugin"), f.path("codex-plugin")))
	if !Audit(f.c).Blocked() {
		t.Fatal("unsafe plugin path accepted")
	}
}

func TestAuditSharedStateAndEmptyProfile(t *testing.T) {
	f := mcpFixture(t)
	f.apply()
	f.write("state/shared/tools", `{"PRIVATE_KEY":"PRIVATE_VALUE"}`)
	a := Audit(f.c)
	if !a.Blocked() {
		t.Fatal("invalid shared MCP accepted")
	}
	b, err := json.Marshal(a)
	must(t, err)
	if strings.Contains(string(b), "PRIVATE_") {
		t.Fatal("shared data leaked")
	}
	f.raw.Resources = []resourceInput{}
	f.load()
	a = Audit(f.c)
	if a.Blocked() || len(a.Resources) != 0 {
		t.Fatal("empty audit is not a no-op")
	}
}
