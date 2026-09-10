package bridge

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMCPCredentialArgumentsFailBeforeWrites(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "mcp", Kind: "mcp-config", Scope: "project", Claude: ".mcp.json", Codex: ".codex/config.toml", AllowReformat: true, Servers: []string{"jenkins"}}}
	f.write(".mcp.json", `{"mcpServers":{"jenkins":{"command":"uvx","args":["mcp-jenkins","--jenkins-password","PRIVATE_FIXTURE_SECRET"]}}}`)
	f.load()
	// Lock acquisition may create an empty state directory. Pre-create it so
	// the snapshot asserts no native writes, journal, baseline or secret copy.
	must(t, os.MkdirAll(f.c.StateDir, 0700))
	before := auditTree(t, f.dir)
	_, err := Apply(f.c, Options{})
	if err == nil || strings.Contains(err.Error(), "PRIVATE_FIXTURE_SECRET") {
		t.Fatalf("credential must be rejected without echoing it: %v", err != nil)
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("rejected credential created state or modified native files")
	}
}

func TestMCPRejectsCredentialArguments(t *testing.T) {
	for _, args := range [][]string{
		{"mcp-jenkins", "--jenkins-password", "PRIVATE_FIXTURE_SECRET"},
		{"--api-key=PRIVATE_FIXTURE_SECRET"},
		{"--access_token", "PRIVATE_FIXTURE_SECRET"},
		{"--Authorization", "Bearer PRIVATE_FIXTURE_SECRET"},
	} {
		for _, side := range []string{"claude", "codex", "shared"} {
			var err error
			if side == "shared" {
				_, err = canonicalServer(MCPServer{Transport: "stdio", Command: "server", Args: args})
			} else {
				x := []any{}
				for _, a := range args {
					x = append(x, a)
				}
				_, err = serverFromNative(side, map[string]any{"command": "server", "args": x})
			}
			if err == nil {
				t.Fatalf("%s accepted credential argument", side)
			}
			if strings.Contains(err.Error(), "PRIVATE_FIXTURE_SECRET") {
				t.Fatal("credential leaked in error")
			}
		}
	}
}

func TestMCPCredentialArgumentCheckKeepsOrdinaryOptions(t *testing.T) {
	for _, args := range [][]string{{"--token-limit", "1000"}, {"-y", "@upstash/context7-mcp@latest"}, {"--password-file", "/private/runtime/password"}} {
		if _, err := canonicalServer(MCPServer{Transport: "stdio", Command: "server", Args: args}); err != nil {
			t.Fatal(err)
		}
	}
}
