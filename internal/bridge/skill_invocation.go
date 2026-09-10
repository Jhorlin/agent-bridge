package bridge

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

type skillBundle struct {
	Entry  *Snapshot `json:"entry"`
	Policy *Snapshot `json:"policy"`
}

func skillPolicyPath(item Item) string {
	return filepath.Join(filepath.Dir(item.Paths["codex"]), "agents", "openai.yaml")
}
func packSkillBundle(entry, policy *Snapshot) (*Snapshot, error) {
	if entry == nil {
		if policy != nil {
			return nil, fmt.Errorf("skill policy has no entry point")
		}
		return nil, nil
	}
	result, err := encoded(skillBundle{entry, policy})
	if err == nil {
		result.Mode = entry.Mode
	}
	return result, err
}
func unpackSkillBundle(raw *Snapshot) (skillBundle, error) {
	var bundle skillBundle
	if raw == nil {
		return bundle, nil
	}
	if err := decodeEnrollmentJSON(raw, &bundle); err != nil || bundle.Entry == nil || valid(bundle.Entry) != nil || valid(bundle.Policy) != nil {
		return bundle, fmt.Errorf("invalid composite skill input")
	}
	return bundle, nil
}
func readItemSide(item Item, side string) (*Snapshot, error) {
	entry, err := snapshot(item.Paths[side])
	if err == nil && item.Adapter == "instruction-set" && side == "claude" {
		alternate, err := snapshot(InstructionCompanionPath(item.Resource))
		if err != nil {
			return nil, err
		}
		return packInstructionBundle(instructionBundle{entry, alternate})
	}
	if err != nil || item.Adapter != "skill-invocation" || side != "codex" {
		return entry, err
	}
	policy, err := snapshot(skillPolicyPath(item))
	if err != nil {
		return nil, err
	}
	return packSkillBundle(entry, policy)
}

func skillInstructionBytes(raw *Snapshot) (string, error) {
	data, err := snapshotBytes(raw)
	if err != nil {
		return "", err
	}
	// Preserve the original instruction bytes, including CRLF, as strict skills do.
	text := string(data)
	body := ""
	for start := strings.IndexByte(text, '\n') + 1; start > 0 && start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			break
		}
		end += start
		if strings.TrimSuffix(text[start:end], "\r") == "---" {
			body = text[end+1:]
			break
		}
		start = end + 1
	}
	return body, nil
}

func invocationFields(raw *Snapshot) (map[string]any, string, error) {
	fields, _, err := agentDocument("claude", raw)
	if err != nil {
		return nil, "", fmt.Errorf("invalid skill invocation metadata")
	}
	body, err := skillInstructionBytes(raw)
	if err != nil {
		return nil, "", err
	}
	for key := range fields {
		if !portableSkillField(key) && key != "disable-model-invocation" {
			return nil, "", fmt.Errorf("unsupported skill invocation field")
		}
	}
	disabled := false
	if value, ok := fields["disable-model-invocation"]; ok {
		var valid bool
		disabled, valid = value.(bool)
		if !valid {
			return nil, "", fmt.Errorf("skill invocation policy requires a boolean")
		}
	}
	commonFields := map[string]any{}
	for key, value := range fields {
		if key != "disable-model-invocation" {
			commonFields[key] = value
		}
	}
	common, err := encodeAgentDocument("claude", commonFields, body)
	if err != nil {
		return nil, "", err
	}
	if _, err := normalizeSkill(common); err != nil {
		return nil, "", err
	}
	fields["disable-model-invocation"] = disabled
	return fields, body, nil
}

func normalizeSkillInvocation(side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	entry := raw
	var policy *Snapshot
	if side == "codex" {
		bundle, err := unpackSkillBundle(raw)
		if err != nil {
			return nil, err
		}
		entry, policy = bundle.Entry, bundle.Policy
		if policy == nil {
			return nil, fmt.Errorf("Codex skill invocation mode requires an explicit policy sidecar; deletion is not an enablement change")
		}
	}
	fields, body, err := invocationFields(entry)
	if err != nil {
		return nil, err
	}
	if side == "codex" {
		// Codex's entry point must remain ordinary common SKILL.md metadata.
		if _, err := normalizeSkill(entry); err != nil {
			return nil, err
		}
		allow := true
		if policy != nil {
			data, err := snapshotBytes(policy)
			if err != nil {
				return nil, err
			}
			wrapped := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte("---\n" + string(data) + "\n---\npolicy\n"))}
			metadata, remainder, err := agentDocument("claude", wrapped)
			if err != nil || remainder != "policy\n" {
				return nil, fmt.Errorf("invalid Codex skill policy YAML")
			}
			p, ok := metadata["policy"].(map[string]any)
			if !ok || len(metadata) != 1 || len(p) != 1 {
				return nil, fmt.Errorf("only policy.allow_implicit_invocation is mapped in this mode")
			}
			allow, ok = p["allow_implicit_invocation"].(bool)
			if !ok {
				return nil, fmt.Errorf("Codex skill invocation policy requires a boolean")
			}
		}
		fields["disable-model-invocation"] = !allow
	}
	result, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		result.Mode = entry.Mode
	}
	return result, err
}

func renderSkillInvocation(side string, content *Snapshot) (*Snapshot, error) {
	if side != "codex" {
		return content, nil
	}
	fields, body, err := invocationFields(content)
	if err != nil {
		return nil, err
	}
	disabled := fields["disable-model-invocation"].(bool)
	delete(fields, "disable-model-invocation")
	entry, err := encodeAgentDocument("claude", fields, body)
	if err != nil {
		return nil, err
	}
	entry.Mode = content.Mode
	policy := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("policy:\n  allow_implicit_invocation: %v\n", !disabled))), Mode: 0600}
	return packSkillBundle(entry, policy)
}

func skillBundleOperations(item Item, before, after *Snapshot) ([]Operation, error) {
	a, err := unpackSkillBundle(before)
	if err != nil {
		return nil, err
	}
	b, err := unpackSkillBundle(after)
	if err != nil {
		return nil, err
	}
	if b.Entry == nil || b.Policy == nil {
		return nil, fmt.Errorf("rendered skill requires entry and policy")
	}
	operation := func(label, path string, old, next *Snapshot) Operation {
		mode := uint32(0600)
		if old != nil {
			mode = old.Mode
		}
		mode = (mode & 0666) | (next.Mode & 0111)
		return Operation{label, path, old, &Snapshot{next.Data, mode}}
	}
	return []Operation{operation(item.Key+"-codex", item.Paths["codex"], a.Entry, b.Entry), operation(item.Key+"-codex-policy", skillPolicyPath(item), a.Policy, b.Policy)}, nil
}
