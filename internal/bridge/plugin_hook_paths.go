package bridge

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Both compatibility-layout native hosts export CLAUDE_PLUGIN_ROOT to hooks.
// Quoting is mandatory so a native cache/source root containing spaces remains
// one executable path. No arguments, shell expressions or other variables are
// accepted. Native trust remains a separate user decision.
var pluginHookExecutable = regexp.MustCompile(`^"\$\{CLAUDE_PLUGIN_ROOT\}/((scripts|hooks)/[A-Za-z0-9_./-]+)"$`)

func pluginHookPath(command string) (string, bool) {
	match := pluginHookExecutable.FindStringSubmatch(command)
	if match == nil || filepath.ToSlash(filepath.Clean(match[1])) != match[1] || strings.EqualFold(match[1], "hooks/hooks.json") {
		return "", false
	}
	return match[1], true
}

// Validate each existing definition against its own authoring tree. Initial
// destinations without hooks are checked through their source and the guarded
// transaction's complete inventory. This never runs the dependency.
func validatePluginHookDependencies(r Resource) error {
	for _, side := range sides {
		raw, err := snapshot(filepath.Join(r.Paths[side], "hooks/hooks.json"))
		if err != nil || raw == nil {
			if err != nil {
				return err
			}
			continue
		}
		canonical, err := normalizeHooksForResource(r, side, raw)
		if err != nil {
			return err
		}
		if canonical == nil {
			continue
		}
		var hooks map[string]any
		if err := decode(canonical, &hooks); err != nil {
			return err
		}
		for _, groups := range hooks {
			for _, group := range groups.([]any) {
				for _, handler := range group.(map[string]any)["hooks"].([]any) {
					command := handler.(map[string]any)["command"].(string)
					if relative, ok := pluginHookPath(command); ok {
						dependency, err := snapshot(filepath.Join(r.Paths[side], relative))
						if err != nil || dependency == nil || dependency.Mode&0111 == 0 {
							return fmt.Errorf("plugin hook requires an existing, safe executable dependency in the same package")
						}
					}
				}
			}
		}
	}
	return nil
}

// Independent native edits can each be valid but merge into a broken package:
// e.g. one host adds a relative hook while the other removes its executable bit.
// Recheck the selected final content, including historical/conflict choices.
func validatePlannedPluginContents(plan PlanResult) error {
	if plan.HasConflicts() {
		return nil // No output can be applied until every choice is reviewed.
	}
	files := map[string]*Snapshot{}
	for _, item := range plan.Items {
		if item.Kind == "plugin-directory" {
			if pluginRootSupportingFile(item.Relative) && item.Content != nil && item.Content.Mode&0111 != 0 {
				return fmt.Errorf("prospective root plugin documentation and images must be non-executable")
			}
			files[item.Key] = item.Content
		}
	}
	for _, item := range plan.Items {
		if item.Kind != "plugin-directory" || item.Adapter != "hook-config" || item.Content == nil {
			continue
		}
		var hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if err := decode(item.Content, &hooks); err != nil {
			return fmt.Errorf("prospective plugin hook cannot be inspected")
		}
		for _, groups := range hooks {
			for _, group := range groups {
				for _, handler := range group.Hooks {
					if relative, ok := pluginHookPath(handler.Command); ok {
						dependency := files[item.ID+"/"+relative]
						if dependency == nil || dependency.Mode&0111 == 0 {
							return fmt.Errorf("prospective plugin hook requires its safe executable package dependency")
						}
					}
				}
			}
		}
	}
	return nil
}
