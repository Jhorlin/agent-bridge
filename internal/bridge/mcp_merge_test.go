package bridge

import (
	"encoding/json"
	"os"
	"testing"
)

func mergeFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "mcp", Kind: "mcp-config", Scope: "project", Claude: "mcp.json", Codex: "config.toml", Servers: []string{"one", "two", "three"}, AllowReformat: true}}
	f.write("mcp.json", `{"unrelated":"keep","mcpServers":{"one":{"command":"first"},"two":{"command":"second"},"three":{"command":"third"},"local":{"command":"private-local"}}}`)
	f.load()
	f.apply()
	return f
}

func changeMCP(t *testing.T, f *fixture, side, name, command string) {
	t.Helper()
	r := f.c.Resources[0]
	before, err := snapshot(r.Paths[side])
	must(t, err)
	common, err := normalizeMCP(r, side, before)
	must(t, err)
	var servers map[string]MCPServer
	must(t, decode(common, &servers))
	s := servers[name]
	s.Command = command
	servers[name] = s
	content, err := encoded(servers)
	must(t, err)
	result, err := renderMCP(r, side, content, before)
	must(t, err)
	must(t, writeSnapshot(r.Paths[side], result))
}

func TestMCPIndependentServerMerge(t *testing.T) {
	f := mergeFixture(t)
	changeMCP(t, f, "claude", "one", "new-first")
	changeMCP(t, f, "codex", "two", "new-second")
	changeMCP(t, f, "shared", "three", "new-third")
	p := f.plan()
	if p.HasConflicts() {
		t.Fatal(p.Summaries())
	}
	f.apply()
	for _, side := range sides {
		raw, err := snapshot(f.c.Resources[0].Paths[side])
		must(t, err)
		common, err := normalizeMCP(f.c.Resources[0], side, raw)
		must(t, err)
		var servers map[string]MCPServer
		must(t, decode(common, &servers))
		if servers["one"].Command != "new-first" || servers["two"].Command != "new-second" || servers["three"].Command != "new-third" {
			t.Fatal(servers)
		}
	}
	var doc map[string]any
	must(t, json.Unmarshal([]byte(f.read("mcp.json")), &doc))
	if doc["unrelated"] != "keep" || doc["mcpServers"].(map[string]any)["local"] == nil {
		t.Fatal("lost unselected fields")
	}
	before := f.read("state/manifest.json")
	f.apply()
	f.expect("state/manifest.json", before)
}

func TestMCPServerConflictsAndDeletion(t *testing.T) {
	for _, mode := range []string{"conflict", "delete", "same"} {
		f := mergeFixture(t)
		changeMCP(t, f, "claude", "one", "new-first")
		if mode == "delete" {
			must(t, os.Remove(f.path("config.toml")))
		} else {
			command := "different"
			if mode == "same" {
				command = "new-first"
			}
			changeMCP(t, f, "codex", "one", command)
		}
		before := f.read("mcp.json")
		manifest := f.read("state/manifest.json")
		_, err := Apply(f.c, Options{})
		if mode == "same" {
			must(t, err)
		} else {
			contains(t, err, "conflicts")
			f.expect("mcp.json", before)
			f.expect("state/manifest.json", manifest)
		}
	}
}

func TestMCPLegacyBaselineAndMergeRollback(t *testing.T) {
	f := mergeFixture(t)
	var m Manifest
	must(t, json.Unmarshal([]byte(f.read("state/manifest.json")), &m))
	for _, name := range f.c.Resources[0].Servers {
		delete(m.Files, mcpBaselineKey("mcp", name))
	}
	data, err := json.Marshal(m)
	must(t, err)
	f.write("state/manifest.json", string(data))
	changeMCP(t, f, "claude", "one", "new-first")
	changeMCP(t, f, "codex", "two", "new-second")
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
	// Resolve to the baseline, then a successful no-op sync seeds per-server hashes.
	changeMCP(t, f, "claude", "one", "first")
	changeMCP(t, f, "codex", "two", "second")
	f.apply()
	changeMCP(t, f, "claude", "one", "new-first")
	changeMCP(t, f, "codex", "two", "new-second")
	manifest := f.read("state/manifest.json")
	a := f.read("mcp.json")
	b := f.read("config.toml")
	shared := f.read("state/shared/mcp")
	_, err = Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("mcp.json", a)
	f.expect("config.toml", b)
	f.expect("state/shared/mcp", shared)
	f.expect("state/manifest.json", manifest)
	f.apply()
}

func TestMCPStaleGranularBaselinesFallBack(t *testing.T) {
	f := mergeFixture(t)
	var m Manifest
	must(t, json.Unmarshal([]byte(f.read("state/manifest.json")), &m))
	m.Files["mcp/mcp-baseline"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data, err := json.Marshal(m)
	must(t, err)
	f.write("state/manifest.json", string(data))
	changeMCP(t, f, "claude", "one", "new-first")
	changeMCP(t, f, "codex", "two", "new-second")
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
}
