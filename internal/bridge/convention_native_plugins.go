package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

const nativePluginInventoryError = "native plugin inventory unavailable; global plugin adoption blocked"
const nativePluginCollisionError = "global plugin has a same-name cached candidate; compare plugin candidates and review native ownership before adoption"

// Presence is deliberately not evidence of installation, enablement, publisher
// equivalence, or authentication. Only fixed conventional cache paths are read.
func nativePluginNames(root string) (map[string]bool, error) {
	names := map[string]bool{}
	remaining := 20000
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if err := assertSafe(path); err != nil {
			return err
		}
		if depth == 0 {
			found, err := pluginIdentityNames(path)
			if err != nil || len(found) == 0 {
				return fmt.Errorf("incomplete plugin identity")
			}
			for name := range found {
				names[name] = true
			}
			return nil
		}
		dir, err := openNoFollow(path)
		if err != nil {
			return err
		}
		before, err := dir.Stat()
		if err != nil || !before.IsDir() {
			dir.Close()
			return fmt.Errorf("unsafe cache directory")
		}
		entries, err := candidateReadDir(dir.ReadDir, remaining)
		remaining -= len(entries)
		after, statErr := dir.Stat()
		closeErr := dir.Close()
		if err != nil || statErr != nil || closeErr != nil || !before.ModTime().Equal(after.ModTime()) {
			return fmt.Errorf("incomplete cache directory")
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe cache entry")
			}
			if err := visit(filepath.Join(path, entry.Name()), depth-1); err != nil {
				return err
			}
		}
		current, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, current) || !before.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("cache changed")
		}
		return nil
	}
	for _, host := range []string{".claude", ".codex"} {
		path := filepath.Join(root, host, "plugins", "cache")
		if err := assertSafe(path); err != nil {
			return nil, fmt.Errorf(nativePluginInventoryError)
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		}
		if err := visit(path, 3); err != nil {
			return nil, fmt.Errorf(nativePluginInventoryError)
		}
	}
	return names, nil
}

// Probe only known identity files; never walk package bodies or follow links.
func pluginIdentityNames(root string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, manifest := range candidateManifests {
		path := filepath.Join(root, manifest.path)
		if err := assertSafe(path); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > candidateManifestLimit {
			return nil, fmt.Errorf("unsafe plugin identity")
		}
		data, err := inventoryRead(path)
		if err != nil {
			return nil, err
		}
		name, err := pluginIdentityName(data)
		if err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, nil
}

func pluginIdentityName(data []byte) (string, error) {
	var doc map[string]any
	if len(data) > candidateManifestLimit || !utf8.Valid(data) || strictJSON(data, &doc) != nil || doc == nil {
		return "", fmt.Errorf("invalid plugin identity")
	}
	if err := validateCandidateIdentity(doc); err != nil {
		return "", err
	}
	if schema, exists := doc["$schema"]; exists && schema != portablePluginSchema {
		return "", fmt.Errorf("unrecognized plugin identity schema")
	}
	return doc["name"].(string), nil
}

func validateDiscoveredNativePlugins(c Config, resources []resourceInput) error {
	if !c.Conventions.ProtectNativePlugins {
		return nil
	}
	names := map[string]string{}
	for _, r := range resources {
		if r.Kind != "plugin-directory" {
			continue
		}
		for _, path := range []string{r.Claude, r.Codex} {
			found, err := pluginIdentityNames(path)
			if err != nil {
				return fmt.Errorf("global plugin identity unavailable; adoption blocked")
			}
			for name := range found {
				if id, exists := names[name]; exists && id != r.ID {
					return fmt.Errorf("global plugins have competing names; review native ownership before adoption")
				}
				names[name] = r.ID
			}
		}
	}
	return validateNativePluginNames(c, names)
}

func validateNativePluginNames(c Config, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	cache, err := nativePluginNames(c.Conventions.Root)
	if err != nil {
		return err
	}
	for name := range names {
		if cache[name] {
			return fmt.Errorf(nativePluginCollisionError)
		}
	}
	return nil
}

func validatePlannedNativePlugins(c Config, plan PlanResult) error {
	if c.Conventions == nil || !c.Conventions.ProtectNativePlugins {
		return nil
	}
	names := map[string]string{}
	for _, item := range plan.Items {
		if !featureResourceID(item.ID) || item.Adapter != "plugin-manifest" || item.Content == nil || item.Conflict != "" {
			continue
		}
		data, err := snapshotBytes(item.Content)
		if err != nil {
			return fmt.Errorf("prospective global plugin identity unavailable; adoption blocked")
		}
		name, err := pluginIdentityName(data)
		if err != nil {
			return fmt.Errorf("prospective global plugin identity unavailable; adoption blocked")
		}
		if id, exists := names[name]; exists && id != item.ID {
			return fmt.Errorf("prospective global plugins have competing names; review native ownership before adoption")
		}
		names[name] = item.ID
	}
	return validateNativePluginNames(c, names)
}
