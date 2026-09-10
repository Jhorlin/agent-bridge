package bridge

import "fmt"

// Plugin packages also use direct server maps. This is deliberately not used
// for mixed global account files, where unrelated objects must never be adopted.
func pluginMCPDocument(raw *Snapshot) (map[string]any, bool, error) {
	doc, err := document("claude", raw)
	if err != nil {
		return nil, false, err
	}
	if value, exists := doc["mcpServers"]; exists {
		entries, ok := value.(map[string]any)
		if !ok || len(doc) != 1 {
			return nil, false, fmt.Errorf("bundled MCP wrapper must contain only mcpServers")
		}
		return entries, false, nil
	}
	for _, value := range doc {
		if _, ok := value.(map[string]any); !ok {
			return nil, false, fmt.Errorf("bundled MCP server must be an object")
		}
	}
	return doc, true, nil
}

func pluginMCPNames(path string) ([]string, error) {
	raw, err := componentSnapshot(path)
	if err != nil || raw == nil {
		return nil, err
	}
	entries, _, err := pluginMCPDocument(raw)
	if err != nil {
		return nil, err
	}
	return conventionalMCPNames(entries)
}

func normalizePluginMCP(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	if side == "shared" {
		return normalizeMCP(r, side, raw)
	}
	entries, _, err := pluginMCPDocument(raw)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(r.Servers) && !featureResourceID(r.ID) {
		return nil, fmt.Errorf("bundled MCP must contain exactly the allowlisted servers")
	}
	for _, name := range r.Servers {
		if _, ok := entries[name]; !ok && !featureResourceID(r.ID) {
			return nil, fmt.Errorf("bundled MCP server missing from allowlist")
		}
	}
	wrapped, err := encoded(map[string]any{"mcpServers": entries})
	if err != nil {
		return nil, err
	}
	return normalizeMCP(r, "claude", wrapped)
}

func renderPluginMCP(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	// New destinations use the wrapped format; existing direct maps keep their
	// layout when reverse edits are rendered, without duplicating server entries.
	direct := false
	if before != nil {
		entries, flat, err := pluginMCPDocument(before)
		if err != nil {
			return nil, err
		}
		direct = flat
		if direct {
			before, err = encoded(map[string]any{"mcpServers": entries})
			if err != nil {
				return nil, err
			}
		}
	}
	out, err := renderMCP(r, "claude", content, before)
	if err != nil || !direct {
		return out, err
	}
	entries, _, err := pluginMCPDocument(out)
	if err != nil {
		return nil, err
	}
	return encoded(entries)
}
