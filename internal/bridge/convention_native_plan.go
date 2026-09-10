package bridge

import "fmt"

// Check the prospective identity, not only metadata already on disk. History,
// canonical-source edits and explicit conflict choices can all change a name.
func validatePlannedNativeCandidates(c Config, plan PlanResult) error {
	if err := validatePlannedNativePlugins(c, plan); err != nil {
		return err
	}
	return validatePlannedNativeSkills(c, plan)
}

func validatePlannedNativeSkills(c Config, plan PlanResult) error {
	if c.Conventions == nil || !c.Conventions.ProtectNativeSkills {
		return nil
	}
	resources := map[string]Resource{}
	for _, r := range c.Resources {
		if r.Kind == "skill-directory" && featureResourceID(r.ID) {
			resources[r.ID] = r
		}
	}
	names := map[string]string{}
	for _, item := range plan.Items {
		if _, ok := resources[item.ID]; !ok || item.Relative != "SKILL.md" || item.Content == nil || item.Conflict != "" {
			continue
		}
		fields, _, err := agentDocument("claude", item.Content)
		if err != nil {
			return fmt.Errorf("prospective global skill metadata cannot be inspected")
		}
		name, ok := fields["name"].(string)
		if !ok || name == "" {
			return fmt.Errorf("prospective global skill name cannot be inspected")
		}
		if previous, ok := names[name]; ok && previous != item.ID {
			return fmt.Errorf("prospective global skills have competing names; review native ownership before adoption")
		}
		names[name] = item.ID
	}
	if len(names) == 0 {
		return nil
	}
	inventory, err := inventorySkillIdentities(c.Conventions.Root)
	if err != nil {
		return fmt.Errorf("native skill inventory unavailable; global skill adoption blocked")
	}
	for _, installed := range inventory {
		id, relevant := names[installed.Name]
		if !relevant {
			continue
		}
		r := resources[id]
		if installed.Status != "identity-inspected" || (installed.Path != r.Paths["claude"] && installed.Path != r.Paths["codex"]) {
			return fmt.Errorf("prospective global skill has another native or cached candidate; review native ownership before adoption")
		}
	}
	return nil
}
