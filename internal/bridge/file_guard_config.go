package bridge

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// FileGuardConfig opts in to reference translation for reviewed path-only
// policies. Scripts and the bridge executable are dependencies, never sync targets.
type FileGuardConfig struct {
	BridgeExecutable string   `json:"bridgeExecutable"`
	Scripts          []string `json:"scripts"`
}

func FileGuardDependencies(r Resource) []string {
	if r.FileGuard == nil {
		return nil
	}
	return append([]string{r.FileGuard.BridgeExecutable}, r.FileGuard.Scripts...)
}

func fileGuardProject(r Resource) string { return filepath.Dir(filepath.Dir(r.Paths["claude"])) }

func validateFileGuardConfig(r Resource, destinations []string) error {
	if r.FileGuard == nil {
		return nil
	}
	project := fileGuardProject(r)
	if r.Scope != "project" || len(r.Links) != 0 || r.Paths["claude"] != filepath.Join(project, ".claude", "settings.json") || r.Paths["codex"] != filepath.Join(project, ".codex", "hooks.json") || len(r.FileGuard.Scripts) == 0 || len(r.FileGuard.Scripts) > 32 {
		return fmt.Errorf("file guard requires unlinked project .claude/settings.json and .codex/hooks.json with 1..32 reviewed scripts")
	}
	seen := map[string]bool{}
	for i, path := range FileGuardDependencies(r) {
		key := strings.ToLower(path)
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") || seen[key] || (i > 0 && (!inside(project, path) || path == project)) {
			return fmt.Errorf("file guard dependencies require distinct clean absolute paths; scripts must be inside the project")
		}
		seen[key] = true
		if err := assertSafe(path); err != nil {
			return err
		}
		for _, target := range destinations {
			target = strings.ToLower(target)
			if inside(target, key) || inside(key, target) {
				return fmt.Errorf("file guard dependency overlaps a managed path")
			}
		}
	}
	return nil
}

func guardQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func guardCommand(r Resource, side, script string) string {
	if side == "claude" {
		return guardQuote(script)
	}
	return guardQuote(r.FileGuard.BridgeExecutable) + " hook-file-guard " + guardQuote(fileGuardProject(r)) + " " + guardQuote(script)
}

func guardSelectedScript(r Resource, side, command string) string {
	for _, script := range r.FileGuard.Scripts {
		if command == guardCommand(r, side, script) {
			return script
		}
		if side == "claude" {
			// Recognize a bounded shell spelling, never evaluate an expression.
			rel, _ := filepath.Rel(fileGuardProject(r), script)
			if hookExecutable.MatchString(script) && command == script {
				return script
			}
			if hookExecutable.MatchString("/"+filepath.ToSlash(rel)) && command == `"$CLAUDE_PROJECT_DIR"/`+filepath.ToSlash(rel) {
				return script
			}
		}
	}
	return ""
}

type guardSelection struct {
	script  string
	handler map[string]any
}

func selectFileGuard(r Resource, side string, doc map[string]any) (guardSelection, error) {
	selected := guardSelection{}
	value, exists := doc["hooks"]
	if !exists {
		return selected, nil
	}
	hooks, ok := value.(map[string]any)
	if !ok {
		return selected, fmt.Errorf("hooks must be an object")
	}
	for event, value := range hooks {
		groups, ok := value.([]any)
		if !ok {
			return selected, fmt.Errorf("hook groups must be arrays")
		}
		for _, value := range groups {
			group, ok := value.(map[string]any)
			if !ok {
				return selected, fmt.Errorf("invalid hook group")
			}
			handlers, ok := group["hooks"].([]any)
			if !ok {
				return selected, fmt.Errorf("invalid hook handlers")
			}
			for _, value := range handlers {
				h, ok := value.(map[string]any)
				if !ok {
					return selected, fmt.Errorf("invalid hook handler")
				}
				command, _ := h["command"].(string)
				script := guardSelectedScript(r, side, command)
				if script == "" {
					if side == "codex" && strings.Contains(command, "hook-file-guard") {
						return selected, fmt.Errorf("unrecognized file guard command; review mapping drift")
					}
					continue
				}
				matcher := "Edit|Write|NotebookEdit"
				if side == "codex" {
					matcher = "^apply_patch$"
				}
				if selected.script != "" || event != "PreToolUse" || group["matcher"] != matcher || len(group) != 2 || h["type"] != "command" {
					return selected, fmt.Errorf("file guard requires one synchronous command with the reviewed exact matcher")
				}
				for key := range h {
					if key != "type" && key != "command" && key != "timeout" {
						return selected, fmt.Errorf("unsupported selected file guard handler field")
					}
				}
				if value, exists := h["timeout"]; exists {
					n, ok := value.(json.Number)
					seconds, err := n.Int64()
					if !ok || err != nil || seconds < 1 || seconds > 600 || (side == "codex" && seconds != 15) {
						return selected, fmt.Errorf("invalid file guard timeout; Codex requires 15 seconds")
					}
				} else if side == "codex" {
					return selected, fmt.Errorf("Codex file guard requires an explicit 15 second timeout")
				}
				selected = guardSelection{script, h}
			}
		}
	}
	return selected, nil
}

func normalizeFileGuard(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	doc, err := document("claude", raw)
	if err != nil {
		return nil, err
	}
	if side == "shared" {
		script, _ := doc["script"].(string)
		if len(doc) != 1 || script == "" {
			return nil, fmt.Errorf("file guard canonical value requires only a reviewed script")
		}
		for _, allowed := range r.FileGuard.Scripts {
			if script == allowed {
				return encoded(doc)
			}
		}
		return nil, fmt.Errorf("file guard script is outside the reviewed allowlist")
	}
	selected, err := selectFileGuard(r, side, doc)
	if err != nil {
		return nil, err
	}
	if selected.script == "" {
		if side == "claude" {
			return nil, fmt.Errorf("Claude settings contain no selected file guard; review mapping drift")
		}
		return nil, nil
	}
	return encoded(map[string]string{"script": selected.script})
}

func renderFileGuard(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	canonical, err := normalizeFileGuard(r, "shared", content)
	if err != nil || canonical == nil {
		return nil, fmt.Errorf("invalid file guard canonical value")
	}
	if side == "shared" {
		return canonical, nil
	}
	var value map[string]string
	if err := decode(canonical, &value); err != nil {
		return nil, err
	}
	doc, err := document("claude", before)
	if err != nil {
		return nil, err
	}
	selected, err := selectFileGuard(r, side, doc)
	if err != nil {
		return nil, err
	}
	command := guardCommand(r, side, value["script"])
	if selected.handler != nil {
		selected.handler["command"] = command
	} else {
		hooks, _ := doc["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
			doc["hooks"] = hooks
		}
		groups, _ := hooks["PreToolUse"].([]any)
		matcher := "Edit|Write|NotebookEdit"
		handler := map[string]any{"type": "command", "command": command}
		if side == "codex" {
			matcher = "^apply_patch$"
			handler["timeout"] = 15
		}
		hooks["PreToolUse"] = append(groups, map[string]any{"matcher": matcher, "hooks": []any{handler}})
	}
	return encoded(doc)
}
