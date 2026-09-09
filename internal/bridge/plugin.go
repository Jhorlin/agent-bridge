package bridge

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

var pluginManifest = map[string]string{"shared": "plugin.json", "claude": ".claude-plugin/plugin.json", "codex": ".codex-plugin/plugin.json"}

const portablePluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

func pluginManifestFor(r Resource, side string) string {
	if side == "codex" && r.CodexPluginLayout == "portable" {
		return "plugin.json"
	}
	return pluginManifest[side]
}

// Package authoring only: this never installs, enables, authenticates, or trusts a plugin.
func expandPlugin(r Resource, m Manifest) ([]Item, error) {
	names := map[string]bool{}
	present := false
	for _, side := range sides {
		files, err := walk(r.Paths[side])
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		present = true
		hasManifest := false
		for _, file := range files {
			if file == pluginManifestFor(r, side) {
				hasManifest = true
				continue
			}
			top := strings.Split(filepath.ToSlash(file), "/")[0]
			switch top {
			case ".mcp.json":
				if file != ".mcp.json" || len(r.Servers) == 0 || !r.AllowReformat || r.CodexPluginLayout == "portable" {
					return nil, fmt.Errorf("unsupported component: bundled MCP requires an explicit servers allowlist, allowReformat and compatibility layout")
				}
				names[file] = true
			case "hooks":
				if r.CodexPluginLayout == "portable" {
					return nil, fmt.Errorf("portable Codex plugin layout does not load bundled hooks; use the compatibility layout")
				}
				if file != "hooks/hooks.json" {
					return nil, fmt.Errorf("only the conventional plugin hooks/hooks.json is supported")
				}
				names[file] = true
			case "skills", "scripts", "assets", "references", "README.md", "LICENSE":
				names[file] = true
			default:
				return nil, fmt.Errorf("plugin %s has an unsupported component (%s); no files will be converted", r.ID, top)
			}
		}
		if !hasManifest {
			return nil, fmt.Errorf("plugin %s requires its native compatibility manifest", r.ID)
		}
		// Every skill must have its own entry point, including on existing targets.
		skillNames := map[string]bool{}
		entrypoints := map[string]bool{}
		for _, file := range files {
			parts := strings.Split(filepath.ToSlash(file), "/")
			if parts[0] == "skills" {
				if len(parts) < 3 {
					return nil, fmt.Errorf("plugin skills must be named directories")
				}
				skillNames[parts[1]] = true
				if len(parts) == 3 && parts[2] == "SKILL.md" {
					entrypoints[parts[1]] = true
				}
			}
		}
		for name := range skillNames {
			if !entrypoints[name] {
				return nil, fmt.Errorf("plugin skill is missing SKILL.md")
			}
		}
	}
	prefix := r.ID + "/"
	for key := range m.Files {
		if strings.HasPrefix(key, prefix) {
			name := strings.TrimPrefix(key, prefix)
			if name != "@manifest" {
				names[name] = true
			}
		}
	}
	if !present {
		return nil, fmt.Errorf("no plugin source exists")
	}
	if len(r.Servers) > 0 && !names[".mcp.json"] {
		return nil, fmt.Errorf("bundled MCP source is missing")
	}
	manifestResource := r
	manifestResource.Paths = map[string]string{}
	for _, side := range sides {
		manifestResource.Paths[side] = filepath.Join(r.Paths[side], pluginManifestFor(r, side))
	}
	result := []Item{{Resource: manifestResource, Key: prefix + "@manifest", Relative: "@manifest", Adapter: "plugin-manifest"}}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		if filepath.IsAbs(name) || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("unsafe plugin path")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, fmt.Errorf("unsafe plugin path")
			}
		}
		entry := r
		entry.Paths = map[string]string{}
		for _, side := range sides {
			entry.Paths[side] = filepath.Join(r.Paths[side], name)
		}
		item := Item{Resource: entry, Key: prefix + name, Relative: name}
		if name == "hooks/hooks.json" {
			item.Adapter = "hook-config"
		}
		if name == ".mcp.json" {
			item.Adapter = "plugin-mcp"
		}
		result = append(result, item)
	}
	return result, nil
}
func normalizePluginManifest(raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	doc, err := document("claude", raw)
	if err != nil {
		return nil, err
	}
	if schema, ok := doc["$schema"]; ok {
		if schema != portablePluginSchema {
			return nil, fmt.Errorf("unsupported portable plugin schema")
		}
		delete(doc, "$schema")
	}
	allowed := map[string]bool{"name": true, "version": true, "description": true, "author": true, "homepage": true, "repository": true, "license": true, "keywords": true, "skills": true}
	for key := range doc {
		if !allowed[key] {
			return nil, fmt.Errorf("plugin manifest has unsupported fields; hooks, MCP, apps, agents, and host-specific metadata require separate adapters")
		}
	}
	name, err := textField(doc, "name")
	if err != nil || !safeID.MatchString(name) {
		return nil, fmt.Errorf("plugin name is required and must be path-safe")
	}
	if value, ok := doc["skills"]; ok {
		if value != "./skills/" && value != "./skills" {
			return nil, fmt.Errorf("custom plugin skill paths are unsupported")
		}
		delete(doc, "skills")
	}
	for _, field := range []string{"version", "description", "homepage", "repository", "license"} {
		if _, err = textField(doc, field); err != nil {
			return nil, err
		}
	}
	if _, err = stringArray(doc["keywords"]); err != nil {
		return nil, err
	}
	if author, ok := doc["author"]; ok {
		m, ok := author.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plugin author must be an object")
		}
		for k := range m {
			if k != "name" && k != "email" && k != "url" {
				return nil, fmt.Errorf("unsupported plugin author field")
			}
			if _, err = textField(m, k); err != nil {
				return nil, err
			}
		}
	}
	return encoded(doc)
}
func renderPluginManifest(r Resource, side string, content *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	doc, err := document("claude", content)
	if err != nil {
		return nil, err
	}
	if side == "codex" && r.CodexPluginLayout == "portable" {
		doc["$schema"] = portablePluginSchema
	} else {
		doc["skills"] = "./skills/"
	}
	return encoded(doc)
}
