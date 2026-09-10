package bridge

import "path/filepath"

// DiagnosticProtectedPaths reads the current profile and its inherited profiles,
// but never inventories native assets or transaction state. Logging calls this
// per event, so discovering a monorepo here would multiply every sync's cost.
// Convention roots conservatively protect future assets regardless of exclusions.
func DiagnosticProtectedPaths(filename string) ([]string, error) {
	c, err := loadConfigMode(filename, true, false)
	if err != nil {
		return nil, err
	}
	paths := append([]string{c.StateDir}, c.ConfigFiles...)
	if c.CoordinationDir != "" {
		paths = append(paths, c.CoordinationDir)
	}
	for _, r := range c.Resources {
		for _, path := range resourceDestinations(r) {
			paths = append(paths, path)
		}
		for _, path := range r.CodexAgentExports {
			paths = append(paths, path)
		}
		for _, link := range r.Links {
			paths = append(paths, link.Path, link.Target)
		}
	}
	if c.Conventions != nil {
		if err := assertSafe(c.Conventions.Root); err != nil {
			return nil, err
		}
		if c.Conventions.Scope == "global" {
			// Protect native roots, not the entire home containing normal OS logs.
			for _, name := range []string{".claude", ".codex", ".agents", ".agent-bridge-plugins", ".claude.json"} {
				paths = append(paths, filepath.Join(c.Conventions.Root, name))
			}
		} else {
			paths = append(paths, c.Conventions.Root)
		}
	}
	return paths, nil
}
