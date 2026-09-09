package bridge

import (
	"fmt"
	"sort"
	"strings"
)

func exportedAgentName(id, name string) string { return "bridge-" + id + "-" + name }

func normalizePluginAgent(item Item, side string, raw *Snapshot) (*Snapshot, error) {
	content, err := normalizeAgentResource(item.Resource, side, raw)
	if err != nil || content == nil {
		return content, err
	}
	var agent portableAgent
	if err := decode(content, &agent); err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(strings.TrimPrefix(item.Relative, "agents/"), ".md")
	expected := name
	if side == "codex" {
		expected = exportedAgentName(item.ID, name)
	}
	if agent.Name != expected {
		return nil, fmt.Errorf("plugin agent name must match its reviewed source/export identity")
	}
	if strings.Contains(agent.Instructions, "${CLAUDE_PLUGIN_ROOT}") || strings.Contains(agent.Instructions, "${PLUGIN_ROOT}") {
		return nil, fmt.Errorf("plugin-root relocation in agent instructions is unsupported")
	}
	agent.Name = name
	return encoded(agent)
}

func renderPluginAgent(item Item, side string, content, before *Snapshot) (*Snapshot, error) {
	if side != "codex" {
		return renderAgentResource(item.Resource, side, content, before)
	}
	var agent portableAgent
	if err := decode(content, &agent); err != nil {
		return nil, err
	}
	agent.Name = exportedAgentName(item.ID, agent.Name)
	value, err := encoded(agent)
	if err != nil {
		return nil, err
	}
	return renderAgentResource(item.Resource, side, value, before)
}

// Complete write footprint for service and separate-copy overlap checks.
func resourceDestinations(r Resource) []string {
	result := []string{}
	for _, path := range r.Paths {
		result = append(result, path)
	}
	for _, path := range r.CodexAgentExports {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
