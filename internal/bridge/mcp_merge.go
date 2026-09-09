package bridge

import "fmt"

func mcpBaselineKey(id, name string) string { return id + "/mcp-server/" + name }

func mcpServerHash(server MCPServer) (string, error) {
	s, err := encoded(server)
	if err != nil {
		return "", err
	}
	return fingerprint(s), nil
}

// Baselines contain hashes only. Native writes remain whole-document operations
// so multiple selected servers never generate competing writes to the same file.
func mergeMCPServers(r Resource, values map[string]*Snapshot, m Manifest) (*Snapshot, string, bool, error) {
	if featureResourceID(r.ID) {
		return mergeConventionMCP(r, values, m)
	}
	// An older writer updates the whole-set hash but cannot update our finer
	// baselines. Fall back until a successful sync seeds consistent hashes again.
	if m.Files[r.ID+"/mcp-baseline"] != m.Files[r.ID] {
		return nil, "", false, nil
	}
	for _, name := range r.Servers {
		if _, ok := m.Files[mcpBaselineKey(r.ID, name)]; !ok {
			return nil, "", false, nil
		}
	}
	entries := map[string]map[string]MCPServer{}
	for _, side := range sides {
		if values[side] == nil {
			return nil, "deletion detected; automatic deletion is disabled", true, nil
		}
		var servers map[string]MCPServer
		if err := decode(values[side], &servers); err != nil {
			return nil, "", true, err
		}
		entries[side] = servers
	}
	merged := map[string]MCPServer{}
	for _, name := range r.Servers {
		base := m.Files[mcpBaselineKey(r.ID, name)]
		chosen := entries["shared"][name]
		changed := ""
		for _, side := range sides {
			server, ok := entries[side][name]
			if !ok {
				return nil, "selected MCP server cannot be deleted", true, nil
			}
			hash, err := mcpServerHash(server)
			if err != nil {
				return nil, "", true, err
			}
			if hash == base {
				continue
			}
			if changed != "" && changed != hash {
				return nil, "concurrent edits differ for the same MCP server", true, nil
			}
			changed = hash
			chosen = server
		}
		merged[name] = chosen
	}
	result, err := encoded(merged)
	return result, "", true, err
}

// Convention mode reconciles initial and newly discovered names independently.
// Absence is allowed only before that server has a baseline; tracked deletion
// always conflicts, including deletion of all native definitions.
func mergeConventionMCP(r Resource, values map[string]*Snapshot, m Manifest) (*Snapshot, string, bool, error) {
	entries := map[string]map[string]MCPServer{}
	for _, side := range sides {
		entries[side] = map[string]MCPServer{}
		if values[side] != nil {
			var servers map[string]MCPServer
			if err := decode(values[side], &servers); err != nil {
				return nil, "", true, err
			}
			entries[side] = servers
		}
	}
	merged := map[string]MCPServer{}
	for _, name := range r.Servers {
		base, tracked := m.Files[mcpBaselineKey(r.ID, name)]
		var chosen MCPServer
		changed := ""
		found := false
		for _, side := range sides {
			server, ok := entries[side][name]
			if !ok {
				if tracked {
					return nil, "tracked MCP server deletion; automatic deletion is disabled", true, nil
				}
				continue
			}
			found = true
			hash, err := mcpServerHash(server)
			if err != nil {
				return nil, "", true, err
			}
			if changed == "" {
				chosen = server
			}
			if tracked && hash == base {
				continue
			}
			if changed != "" && changed != hash {
				return nil, "different definitions for the same MCP server", true, nil
			}
			changed = hash
			chosen = server
		}
		if !found {
			return nil, "tracked MCP server has no source", true, nil
		}
		merged[name] = chosen
	}
	raw, err := encoded(merged)
	return raw, "", true, err
}

func recordMCPBaselines(r Resource, content *Snapshot, m *Manifest) error {
	var servers map[string]MCPServer
	if err := decode(content, &servers); err != nil {
		return err
	}
	for _, name := range r.Servers {
		server, ok := servers[name]
		if !ok {
			return fmt.Errorf("missing selected MCP server baseline")
		}
		hash, err := mcpServerHash(server)
		if err != nil {
			return err
		}
		m.Files[mcpBaselineKey(r.ID, name)] = hash
	}
	m.Files[r.ID+"/mcp-baseline"] = fingerprint(content)
	return nil
}
