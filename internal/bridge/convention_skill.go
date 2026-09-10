package bridge

import (
	"encoding/base64"
	"fmt"
	"regexp"
)

// This is a conservative automatic-adoption boundary, not a host interpreter.
// Reserved prefixes (including escaped forms) require explicit compatibility
// review; word boundaries would miss argument suffixes and incomplete tokens.
var hostSkillExpansion = regexp.MustCompile("\\$(?:ARGUMENTS|[0-9]|\\{?CLAUDE_)|!`|```!")

func normalizeConventionSkill(item Item, side string, raw *Snapshot, m Manifest) (*Snapshot, error) {
	if featureResourceID(item.ID) && raw != nil {
		entry := raw
		if side == "codex" {
			bundle, err := unpackSkillBundle(raw)
			if err != nil {
				return nil, err
			}
			entry = bundle.Entry
		}
		data, err := snapshotBytes(entry)
		if err != nil {
			return nil, err
		}
		if hostSkillExpansion.Match(data) {
			return nil, fmt.Errorf("conventional skill contains host-only argument, variable or shell expansion; explicit compatibility review required")
		}
	}
	if !featureResourceID(item.ID) || side != "codex" || raw == nil {
		return normalizeSkillResource(item.Resource, side, raw)
	}
	bundle, err := unpackSkillBundle(raw)
	if err != nil {
		return nil, err
	}
	if bundle.Policy == nil {
		if _, tracked := m.Files["@skill-policy/"+item.Key]; tracked {
			return nil, fmt.Errorf("tracked skill policy sidecar deleted")
		}
		// A pre-existing ordinary Codex skill uses implicit invocation by default.
		bundle.Policy = &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte("policy:\n  allow_implicit_invocation: true\n")), Mode: 0600}
		raw, err = packSkillBundle(bundle.Entry, bundle.Policy)
		if err != nil {
			return nil, err
		}
	}
	return normalizeSkillResource(item.Resource, side, raw)
}

func recordConventionSkillPolicy(item Item, m *Manifest) error {
	bundle, err := unpackSkillBundle(item.Values["codex"])
	if err != nil {
		return err
	}
	if hasField(item.Writes, "codex") {
		raw, err := renderSkillInvocation("codex", item.Content)
		if err != nil {
			return err
		}
		bundle, err = unpackSkillBundle(raw)
		if err != nil {
			return err
		}
	}
	if bundle.Policy != nil {
		m.Files["@skill-policy/"+item.Key] = fingerprint(bundle.Policy)
	}
	return nil
}
