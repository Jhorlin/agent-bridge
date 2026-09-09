package bridge

import "fmt"

func normalizePluginMCP(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	if side == "shared" {
		return normalizeMCP(r, side, raw)
	}
	doc, err := document("claude", raw)
	if err != nil {
		return nil, err
	}
	entries, ok := doc["mcpServers"].(map[string]any)
	if len(doc) != 1 || !ok || len(entries) != len(r.Servers) {
		return nil, fmt.Errorf("bundled MCP must contain exactly the allowlisted servers")
	}
	for _, name := range r.Servers {
		if _, ok := entries[name]; !ok {
			return nil, fmt.Errorf("bundled MCP server missing from allowlist")
		}
	}
	return normalizeMCP(r, "claude", raw)
}

func renderPluginMCP(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	// Both native compatibility packages use the conventional JSON format.
	return renderMCP(r, "claude", content, before)
}
