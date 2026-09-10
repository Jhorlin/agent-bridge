package bridge

import (
	"fmt"
	"strings"
)

// These are retained only in the existing Claude source. In particular,
// allowed-tools is a per-turn grant, not an availability restriction. Never
// translate it into persistent Codex approval rules or claim permission parity.
func splitSkillSettings(raw *Snapshot) (*Snapshot, map[string]any, error) {
	fields, _, err := agentDocument("claude", raw)
	if err != nil {
		return nil, nil, err
	}
	local := map[string]any{}
	validText := func(v any) bool {
		s, ok := v.(string)
		return ok && strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\r\n")
	}
	for key, value := range fields {
		valid := false
		switch key {
		case "allowed-tools":
			valid = validText(value)
			if list, ok := value.([]any); ok {
				valid = true
				for _, v := range list {
					valid = valid && validText(v)
				}
			}
		case "argument-hint", "version":
			valid = validText(value)
		case "mcp":
			if list, ok := value.([]any); ok {
				valid = true
				for _, v := range list {
					valid = valid && validText(v)
				}
			}
		default:
			continue // The strict portable projection rejects other fields.
		}
		if !valid {
			return nil, nil, fmt.Errorf("invalid native-local skill setting")
		}
		local[key] = value
		delete(fields, key)
	}
	body, err := skillInstructionBytes(raw)
	if err != nil {
		return nil, nil, err
	}
	clean, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		clean.Mode = raw.Mode
	}
	return clean, local, err
}

func normalizeSkillResource(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if r.PreserveSkillSettings && side == "claude" && raw != nil {
		clean, _, err := splitSkillSettings(raw)
		if err != nil {
			return nil, err
		}
		raw = clean
	}
	return normalizeSkillInvocation(side, raw)
}

func renderSkillResource(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	result, err := renderSkillInvocation(side, content)
	if err != nil || !r.PreserveSkillSettings || side != "claude" || before == nil {
		return result, err
	}
	_, local, err := splitSkillSettings(before)
	if err != nil {
		return nil, err
	}
	fields, _, err := agentDocument("claude", result)
	if err != nil {
		return nil, err
	}
	body, err := skillInstructionBytes(result)
	if err != nil {
		return nil, err
	}
	for key, value := range local {
		fields[key] = value
	}
	return encodeAgentDocument("claude", fields, body)
}
