package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Resolution choices are exact item keys mapped to a native/shared side. They
// resolve conflicts only; they cannot force an otherwise clean item to rewind.
func resolvePlan(p *PlanResult, choices map[string]string) error {
	remaining := len(choices)
	for n := range p.Items {
		i := &p.Items[n]
		side, chosen := choices[i.Key]
		if !chosen {
			if i.Conflict != "" {
				return fmt.Errorf("every conflict requires an explicit choice")
			}
			continue
		}
		remaining--
		if i.Conflict == "" {
			return fmt.Errorf("resolution choice must identify a conflicted item")
		}
		if side != "shared" && side != "claude" && side != "codex" {
			return fmt.Errorf("resolution requires shared, claude or codex")
		}
		content, err := resolutionContent(*i, side)
		if err != nil {
			return err
		}
		if content == nil {
			return fmt.Errorf("selected side is absent; deletion requires a separate workflow")
		}
		i.Content, i.Digest, i.Conflict = content, fingerprint(content), ""
		i.Writes = []string{}
		for _, target := range sides {
			current, err := resolutionContent(*i, target)
			if err != nil {
				return err
			}
			if fingerprint(current) != i.Digest {
				i.Writes = append(i.Writes, target)
			}
		}
	}
	if remaining != 0 || len(choices) == 0 {
		return fmt.Errorf("resolution choices must identify existing conflicts")
	}
	return nil
}

func resolutionContent(i Item, side string) (*Snapshot, error) {
	raw := i.Values[side]
	switch i.Adapter {
	case "skill-invocation":
		return normalizeSkillInvocation(side, raw)
	case "plugin-command":
		return normalizePluginCommand(raw)
	case "skill-metadata":
		return normalizeSkill(raw)
	case "instruction-file":
		return normalizeInstructions(side, raw)
	case "agent-file":
		return normalizeAgentResource(i.Resource, side, raw)
	case "hook-config":
		return normalizeHooks(side, raw)
	case "mcp":
		return normalizeMCP(i.Resource, side, raw)
	case "plugin-mcp":
		return normalizePluginMCP(i.Resource, side, raw)
	case "plugin-manifest":
		return normalizePluginManifest(raw)
	case "":
		return raw, nil
	default:
		return nil, fmt.Errorf("unsupported resolution adapter")
	}
}

func resolutionObservation(c Config, p PlanResult, choices map[string]string) string {
	data, _ := json.Marshal(struct {
		Observation string
		Choices     map[string]string
	}{Observation(c, p), choices})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ReviewResolution previews an explicit decision without writing or acquiring
// locks. Other non-conflicting pending changes in the profile are included.
func ReviewResolution(filename string, choices map[string]string) (ReviewCheckpoint, error) {
	r := ReviewCheckpoint{Version: 1, ReadOnly: true}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return r, err
	}
	p, err := Plan(c)
	if err != nil {
		return r, err
	}
	r.Observation = resolutionObservation(c, p, choices)
	if err := resolvePlan(&p, choices); err != nil {
		return r, err
	}
	r.Summaries = p.Summaries()
	return r, nil
}

func ResolveReviewed(filename, observation string, choices map[string]string) ([]Summary, error) {
	if !digestPattern.MatchString(observation) || len(choices) == 0 {
		return nil, fmt.Errorf("resolution requires a review digest and explicit choices")
	}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return nil, err
	}
	return Apply(c, Options{ExpectedObservation: observation, Resolutions: choices})
}
