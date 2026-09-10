package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// The common model must not turn Codex's working-directory override into a
// Claude field that the native host silently ignores, or adopt such old state.
func TestMCPCWDRejectedBeforeWrites(t *testing.T) {
	for _, side := range []string{"claude", "codex", "shared"} {
		for _, cwd := range []string{"/fixture/private-cwd-value", "relative", " ", "${CLAUDE_PLUGIN_ROOT}"} {
			t.Run(side+"/"+cwd, func(t *testing.T) {
				f := mcpFixture(t)
				f.apply()
				path := map[string]string{"claude": "claude.json", "codex": "codex.toml", "shared": "state/shared/tools"}[side]
				value, err := json.Marshal(cwd)
				must(t, err)
				switch side {
				case "claude":
					f.write(path, `{"mcpServers":{"docs":{"command":"demo-server","cwd":`+string(value)+`}}}`)
				case "codex":
					f.write(path, "[mcp_servers.docs]\ncommand='demo-server'\ncwd="+string(value)+"\n")
				case "shared":
					f.write(path, `{"docs":{"transport":"stdio","command":"demo-server","cwd":`+string(value)+`}}`)
				}
				before := map[string]string{}
				for _, name := range []string{"claude.json", "codex.toml", "state/shared/tools", "state/manifest.json"} {
					before[name] = f.read(name)
				}
				wrote := false
				_, err = Apply(f.c, Options{BeforeWrite: func(int, Operation) error { wrote = true; return nil }})
				contains(t, err, "cwd")
				if strings.Contains(err.Error(), "private-cwd-value") {
					t.Fatal("unsupported path leaked into error")
				}
				if wrote {
					t.Fatal("unsupported cwd reached the write phase")
				}
				for name, contents := range before {
					f.expect(name, contents)
				}
				f.missing("state/pending.json")
			})
		}
	}
}

func TestMCPLegacySharedCWDRemainsReadableButRejected(t *testing.T) {
	// Keep the existing encoding readable: removing the field would silently
	// lose old state through ordinary decoding before adapters can reject it.
	raw, err := encoded(map[string]any{"docs": map[string]any{"transport": "stdio", "command": "server", "cwd": "/fixture/old-work"}})
	must(t, err)
	var decoded map[string]MCPServer
	must(t, decode(raw, &decoded))
	if decoded["docs"].CWD != "/fixture/old-work" {
		t.Fatal("legacy cwd was silently discarded")
	}
	_, err = normalizeMCP(Resource{ID: "tools", Servers: []string{"docs"}}, "shared", raw)
	contains(t, err, "cwd")
}
