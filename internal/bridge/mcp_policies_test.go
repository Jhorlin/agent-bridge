package bridge

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Synthetic policy values exercise the documented Codex field contract; this
// is not a captured user configuration and starts no server.
const mcpPolicies = `
enabled = false
required = true
enabled_tools = ["read", "write"]
disabled_tools = ["write"]
startup_timeout_sec = 20.5
tool_timeout_sec = 45
default_tools_approval_mode = "prompt"
[mcp_servers.docs.tools.read]
approval_mode = "approve"
output_token_limit = 3000
`

func mcpPolicyFixture(t *testing.T) *fixture {
	f := mcpFixture(t)
	f.raw.Resources[0].PreserveCodexMCPPolicies = true
	f.load()
	f.write("codex.toml", "[mcp_servers.docs]\ncommand = 'demo-server'\nargs = ['--safe']\nenv_vars = ['DEMO_TOKEN']\n"+mcpPolicies)
	return f
}

func TestMCPPoliciesPreservedButNotShared(t *testing.T) {
	f := mcpPolicyFixture(t)
	if Audit(f.c).Blocked() {
		t.Fatal("opted-in policies blocked")
	}
	f.apply()
	original, err := document("codex", mustSnapshot(t, f.path("codex.toml")))
	must(t, err)
	f.write("claude.json", strings.Replace(f.read("claude.json"), "demo-server", "updated-server", 1))
	before := f.read("codex.toml")
	_, err = Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("codex.toml", before)
	f.apply()
	current, err := document("codex", mustSnapshot(t, f.path("codex.toml")))
	must(t, err)
	server := func(doc map[string]any) map[string]any {
		return doc["mcp_servers"].(map[string]any)["docs"].(map[string]any)
	}
	_, oldPolicies, err := splitCodexMCPPolicies(server(original))
	must(t, err)
	_, newPolicies, err := splitCodexMCPPolicies(server(current))
	must(t, err)
	if !reflect.DeepEqual(oldPolicies, newPolicies) {
		t.Fatal("local policy changed")
	}
	for _, path := range []string{"claude.json", "state/shared/tools"} {
		for _, field := range codexMCPPolicyFields {
			if strings.Contains(f.read(path), `"`+field+`"`) {
				t.Fatal("policy leaked", path, field)
			}
		}
	}
	// A policy-only local edit is independent of transport synchronization.
	f.write("codex.toml", strings.Replace(f.read("codex.toml"), "required = true", "required = false", 1))
	tree := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(tree, auditTree(t, f.dir)) {
		t.Fatal("policy-only edit caused sync")
	}
	f.write("codex.toml", strings.Replace(f.read("codex.toml"), "updated-server", "reverse-server", 1))
	f.apply()
	if !strings.Contains(f.read("claude.json"), "reverse-server") {
		t.Fatal("reverse transport edit missing")
	}
}

func TestMCPPoliciesFailClosedAndBindReview(t *testing.T) {
	f := mcpPolicyFixture(t)
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("codex.toml", strings.Replace(f.read("codex.toml"), "enabled = false", "enabled = true", 1))
	_, err = SyncReviewed(f.path("config.json"), r.Observation)
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatal(err)
	}
	f.raw.Resources[0].PreserveCodexMCPPolicies = false
	f.load()
	if !Audit(f.c).Blocked() {
		t.Fatal("policies allowed without opt-in")
	}
	g := newFixture(t)
	g.raw.Resources[0].PreserveCodexMCPPolicies = true
	g.save()
	if _, err := LoadAuditConfig(g.path("config.json")); err == nil {
		t.Fatal("flag accepted for wrong kind")
	}
}

func TestMCPPolicyConsentIsResourceIdentity(t *testing.T) {
	f := mcpPolicyFixture(t)
	f.apply()
	f.raw.Resources[0].PreserveCodexMCPPolicies = false
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("policy consent silently changed")
	}
}

func TestMCPPolicyMalformedAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		"enabled = 'false'", "enabled_tools = 'read'", "enabled_tools = ['read', 'read']", "enabled_tools = ['']",
		"startup_timeout_sec = -1", "startup_timeout_sec = nan", "tool_timeout_sec = 86401", "tool_timeout_sec = '60'",
		"default_tools_approval_mode = 'unknown'", "tools = 'invalid'", "[mcp_servers.docs.tools.read]\nunknown = true",
		"[mcp_servers.docs.tools.read]\napproval_mode = 'invalid'", "[mcp_servers.docs.tools.read]\noutput_token_limit = 0",
		"http_headers_helper = '/private/helper'", "auth = 'chatgpt'",
	} {
		f := mcpPolicyFixture(t)
		f.write("codex.toml", "[mcp_servers.docs]\ncommand = 'demo-server'\n"+body+"\n")
		if !Audit(f.c).Blocked() {
			t.Fatal("invalid policy accepted", body)
		}
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("wrote invalid policy")
		}
		f.expect("claude.json", `{"mcpServers":{"docs":{"command":"demo-server","args":["--safe"],"env":{"DEMO_TOKEN":"${DEMO_TOKEN}"}}}}`)
	}
}

func TestNativeMCPPolicyPreservation(t *testing.T) {
	f := mcpPolicyFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "codex-home/config.toml"
	f.load()
	f.write("codex-home/config.toml", f.read("codex.toml"))
	f.apply()
	f.write("claude.json", strings.Replace(f.read("claude.json"), "demo-server", "updated-server", 1))
	f.apply()
	output := nativeRun(t, f, tools["codex"], "mcp", "get", "docs", "--json")
	var result map[string]any
	must(t, json.Unmarshal([]byte(output), &result))
	if result["enabled"] != false {
		t.Fatal("Codex did not retain disabled policy", output)
	}
	if !strings.Contains(output, "updated-server") || !strings.Contains(output, "read") {
		t.Fatal("Codex did not load preserved policy", output)
	}
}
