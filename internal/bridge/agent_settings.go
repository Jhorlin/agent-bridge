package bridge

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func agentDocument(side string, raw *Snapshot) (map[string]any, string, error) {
	if raw == nil {
		return map[string]any{}, "", nil
	}
	if side == "codex" {
		doc, err := document(side, raw)
		return doc, "", err
	}
	data, err := snapshotBytes(raw)
	if err != nil || !utf8.Valid(data) {
		return nil, "", fmt.Errorf("invalid agent text")
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", fmt.Errorf("agent requires frontmatter")
	}
	parts := strings.SplitN(text[4:], "\n---\n", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("agent frontmatter unterminated")
	}
	d := yaml.NewDecoder(strings.NewReader(parts[0]))
	var node yaml.Node
	if err := d.Decode(&node); err != nil || len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, "", fmt.Errorf("invalid agent frontmatter")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, "", fmt.Errorf("multiple agent documents unsupported")
	}
	var check func(*yaml.Node) bool
	check = func(n *yaml.Node) bool {
		if n.Anchor != "" || n.Kind == yaml.AliasNode {
			return false
		}
		if n.Tag != "!!map" && n.Tag != "!!seq" && n.Tag != "!!str" && n.Tag != "!!int" && n.Tag != "!!bool" {
			return false
		}
		for _, child := range n.Content {
			if !check(child) {
				return false
			}
		}
		return true
	}
	if !check(node.Content[0]) {
		return nil, "", fmt.Errorf("unsupported agent YAML construct")
	}
	fields := map[string]any{}
	if err := node.Decode(&fields); err != nil {
		return nil, "", fmt.Errorf("invalid or duplicate agent fields")
	}
	return fields, parts[1], nil
}

func encodeAgentDocument(side string, fields map[string]any, body string) (*Snapshot, error) {
	var data []byte
	var err error
	if side == "codex" {
		data, err = toml.Marshal(fields)
	} else {
		data, err = yaml.Marshal(fields)
		data = []byte("---\n" + string(data) + "---\n" + body)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot encode agent settings")
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString(data), Mode: 0600}, nil
}

func splitAgentSettings(side string, fields map[string]any) (map[string]any, map[string]any, error) {
	portable, local := map[string]any{}, map[string]any{}
	for key, value := range fields {
		if key == "name" || key == "description" || (side == "codex" && key == "developer_instructions") {
			portable[key] = value
			continue
		}
		valid := false
		if key == "model" {
			s, ok := value.(string)
			valid = ok && strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\r\n")
		}
		if side == "claude" {
			switch key {
			case "permissionMode":
				valid = agentSettingChoice(value, []string{"default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "plan", "manual"})
			case "maxTurns":
				n, ok := value.(int)
				valid = ok && n > 0
			case "tools", "disallowedTools":
				if s, ok := value.(string); ok {
					valid = strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\r\n")
				} else if a, ok := value.([]any); ok {
					valid = true
					for _, entry := range a {
						s, ok := entry.(string)
						if !ok || strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\x00\r\n") {
							valid = false
						}
					}
				}
			}
		} else {
			switch key {
			case "model_reasoning_effort":
				valid = agentSettingChoice(value, []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"})
			case "sandbox_mode":
				valid = agentSettingChoice(value, []string{"read-only", "workspace-write", "danger-full-access"})
			case "approval_policy":
				valid = agentSettingChoice(value, []string{"untrusted", "on-failure", "on-request", "never"})
			}
		}
		if !valid {
			return nil, nil, fmt.Errorf("unsupported or invalid host-local agent setting")
		}
		local[key] = value
	}
	return portable, local, nil
}

func agentSettingChoice(value any, choices []string) bool {
	s, ok := value.(string)
	return ok && hasField(choices, s)
}

func normalizeAgentResource(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if !r.PreserveAgentSettings || side == "shared" || raw == nil {
		return normalizeAgent(side, raw)
	}
	fields, body, err := agentDocument(side, raw)
	if err != nil {
		return nil, err
	}
	portable, _, err := splitAgentSettings(side, fields)
	if err != nil {
		return nil, err
	}
	clean, err := encodeAgentDocument(side, portable, body)
	if err != nil {
		return nil, err
	}
	return normalizeAgent(side, clean)
}

func renderAgentResource(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	result, err := renderAgent(side, content)
	if err != nil || !r.PreserveAgentSettings || side == "shared" || before == nil {
		return result, err
	}
	fields, _, err := agentDocument(side, before)
	if err != nil {
		return nil, err
	}
	_, local, err := splitAgentSettings(side, fields)
	if err != nil {
		return nil, err
	}
	output, body, err := agentDocument(side, result)
	if err != nil {
		return nil, err
	}
	for key, value := range local {
		output[key] = value
	}
	return encodeAgentDocument(side, output, body)
}
