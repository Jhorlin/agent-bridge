package bridge

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Both compatibility-layout native hosts export CLAUDE_PLUGIN_ROOT to hooks.
// Quoting is mandatory so a native cache/source root containing spaces remains
// one path. Only package-file arguments and the sh/bash interpreters are
// accepted. Native trust remains a separate user decision.
var pluginHookExecutable = regexp.MustCompile(`^"\$\{CLAUDE_PLUGIN_ROOT\}/((scripts|hooks|hooks-handlers)/[A-Za-z0-9_./-]+)"$`)

func pluginHookPath(command string) (string, bool) {
	match := pluginHookExecutable.FindStringSubmatch(command)
	if match == nil || filepath.ToSlash(filepath.Clean(match[1])) != match[1] || strings.EqualFold(match[1], "hooks/hooks.json") {
		return "", false
	}
	return match[1], true
}

type pluginHookDependency struct {
	Path       string
	Executable bool
}

// Deliberately not a general shell parser: each token is a quoted, clean
// package-relative file. No flags, substitutions, pipelines or environment
// assignments. An interpreter reads its first file, so that file need not have
// executable bits; direct execution still requires them.
func pluginHookDependencies(command string) ([]pluginHookDependency, bool) {
	parts := strings.Split(command, " ")
	interpreted := parts[0] == "sh" || parts[0] == "bash"
	if interpreted {
		parts = parts[1:]
	}
	if len(parts) == 0 || len(parts) > 16 {
		return nil, false
	}
	if !interpreted && len(parts) != 1 {
		return nil, false
	}
	dependencies := make([]pluginHookDependency, 0, len(parts))
	for index, part := range parts {
		path, ok := pluginHookPath(part)
		if !ok {
			return nil, false
		}
		dependencies = append(dependencies, pluginHookDependency{Path: path, Executable: index == 0 && !interpreted})
	}
	return dependencies, true
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
					dependencies, _ := pluginHookDependencies(command)
					for _, reference := range dependencies {
						dependency, err := snapshot(filepath.Join(r.Paths[side], reference.Path))
						if err != nil || dependency == nil || (reference.Executable && dependency.Mode&0111 == 0) {
							return fmt.Errorf("plugin hook requires an existing, safe package dependency with executable bits for direct execution")
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
			if item.Relative == "skills/.gitkeep" && item.Content != nil && (item.Content.Data != "" || item.Content.Mode&0111 != 0) {
				return fmt.Errorf("prospective skill placeholder must be empty and non-executable")
			}
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
					dependencies, _ := pluginHookDependencies(handler.Command)
					for _, reference := range dependencies {
						dependency := files[item.ID+"/"+reference.Path]
						if dependency == nil || (reference.Executable && dependency.Mode&0111 == 0) {
							return fmt.Errorf("prospective plugin hook requires its safe package dependency with executable bits for direct execution")
						}
					}
				}
			}
		}
	}
	return nil
}
