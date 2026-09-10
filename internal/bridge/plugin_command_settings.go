package bridge

import (
	"fmt"
	"strings"
)

// Retention is not cross-host policy translation. No Claude grant or model
// selection is exported to Codex's command migration or shared canonical text.
func splitPluginCommandSettings(raw *Snapshot) (*Snapshot, map[string]any, error) {
	fields, body, err := agentDocument("claude", raw)
	if err != nil {
		return nil, nil, err
	}
	local := map[string]any{}
	validText := func(value any) bool {
		s, ok := value.(string)
		return ok && strings.TrimSpace(s) != "" && len(s) <= 4096 && !strings.ContainsAny(s, "\x00\r\n")
	}
	for key, value := range fields {
		valid := false
		switch key {
		case "model":
			valid = validText(value)
		case "allowed-tools", "argument-hint":
			valid = validText(value)
			if entries, ok := value.([]any); ok {
				valid = len(entries) <= 128
				if key == "argument-hint" {
					valid = valid && len(entries) > 0
				}
				for _, entry := range entries {
					valid = valid && validText(entry)
				}
			}
		case "disable-model-invocation":
			// Explicit false is the native default, verified on both hosts.
			// True is an execution policy and cannot be dropped on migration.
			flag, ok := value.(bool)
			valid = ok && !flag
		default:
			continue // Strict command normalization rejects every other field.
		}
		if !valid {
			return nil, nil, fmt.Errorf("invalid host-local plugin command setting")
		}
		local[key] = value
		delete(fields, key)
	}
	clean, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		clean.Mode = raw.Mode
	}
	return clean, local, err
}

func normalizePluginCommandResource(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if r.PreserveCommandSettings && side == "claude" && raw != nil {
		clean, _, err := splitPluginCommandSettings(raw)
		if err != nil {
			return nil, err
		}
		raw = clean
	}
	return normalizePluginCommand(raw)
}

func renderPluginCommandResource(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	if !r.PreserveCommandSettings || side != "claude" || before == nil || content == nil {
		return content, nil
	}
	_, local, err := splitPluginCommandSettings(before)
	if err != nil {
		return nil, err
	}
	fields, body, err := agentDocument("claude", content)
	if err != nil {
		return nil, err
	}
	for key, value := range local {
		fields[key] = value
	}
	result, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		result.Mode = content.Mode
	}
	return result, err
}
