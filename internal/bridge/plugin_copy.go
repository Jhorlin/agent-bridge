package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type PluginCopyReport struct {
	Matches          bool     `json:"matches"`
	SourceDigest     string   `json:"sourceDigest"`
	CopyDigest       string   `json:"copyDigest"`
	Missing          []string `json:"missing"`
	Changed          []string `json:"changed"`
	Unexpected       []string `json:"unexpected"`
	GeneratedIgnored int      `json:"generatedIgnored"`
	Warning          string   `json:"warning"`
}

// ComparePluginCopy reads only an explicitly selected package copy. It never
// discovers caches, launches a host or infers installation/enablement/trust state.
func ComparePluginCopy(filename, id, side, copyRoot string) (PluginCopyReport, error) {
	result := PluginCopyReport{Missing: []string{}, Changed: []string{}, Unexpected: []string{}, Warning: "This compares authoring files, not installation, enablement, authentication or hook trust. Generated migration files are not certified."}
	c, err := LoadConfig(filename)
	if err != nil {
		return result, err
	}
	if side != "claude" && side != "codex" {
		return result, fmt.Errorf("select a native package side")
	}
	if !filepath.IsAbs(copyRoot) || filepath.Clean(copyRoot) != copyRoot {
		return result, fmt.Errorf("copy root must be clean and absolute")
	}
	if err := assertSafe(copyRoot); err != nil {
		return result, err
	}
	var r Resource
	for _, candidate := range c.Resources {
		if candidate.ID == id {
			r = candidate
		}
	}
	if r.Kind != "plugin-directory" {
		return result, fmt.Errorf("select a plugin resource")
	}
	if len(r.CodexAgentExports) > 0 {
		result.Warning += " Standalone Codex agent exports are outside this package-copy comparison."
	}
	for _, root := range resourceDestinations(r) {
		a, b := strings.ToLower(root), strings.ToLower(copyRoot)
		if inside(a, b) || inside(b, a) {
			return result, fmt.Errorf("copy must be separate from managed authoring roots")
		}
	}
	// Actual adapters validate the registered content, including mixed manifests,
	// MCP and hooks. Conflicts may exist: this report compares the selected side.
	if _, err := Plan(c); err != nil {
		return result, err
	}
	manifest, err := snapshot(filepath.Join(copyRoot, pluginManifestFor(r, side)))
	if err != nil || manifest == nil {
		return result, fmt.Errorf("copy compatibility manifest is missing or unsafe")
	}
	if _, err := normalizePluginManifest(manifest); err != nil {
		return result, err
	}
	files, err := walk(r.Paths[side])
	if err != nil {
		return result, err
	}
	if len(files) == 0 {
		return result, fmt.Errorf("selected authoring package is missing")
	}
	copies, err := walk(copyRoot)
	if err != nil {
		return result, err
	}
	source, actual := map[string]string{}, map[string]string{}
	for _, name := range files {
		before, err := snapshot(filepath.Join(r.Paths[side], name))
		if err != nil || before == nil {
			return result, fmt.Errorf("authoring package changed during comparison")
		}
		after, err := snapshot(filepath.Join(copyRoot, name))
		if err != nil {
			return result, err
		}
		source[name], actual[name] = fingerprint(before), fingerprint(after)
		if after == nil {
			result.Missing = append(result.Missing, name)
		} else if source[name] != actual[name] {
			result.Changed = append(result.Changed, name)
		}
	}
	for _, name := range copies {
		if _, exists := source[name]; exists {
			continue
		}
		if side == "codex" && strings.HasPrefix(filepath.ToSlash(name), ".codex-plugin/migrated-command-skills/") {
			result.GeneratedIgnored++
			continue
		}
		result.Unexpected = append(result.Unexpected, name)
		// Do not read unknown extra files: they might contain host-local secrets.
		actual[name] = "unexpected-unread"
	}
	// A second inventory/raw-hash comparison detects ordinary concurrent edits;
	// this remains a point-in-time report, not a lock on native host state.
	again, err := walk(r.Paths[side])
	if err != nil || !sameStringList(files, again) {
		return result, fmt.Errorf("authoring inventory changed")
	}
	again, err = walk(copyRoot)
	if err != nil || !sameStringList(copies, again) {
		return result, fmt.Errorf("copy inventory changed")
	}
	for _, name := range files {
		a, err := snapshot(filepath.Join(r.Paths[side], name))
		if err != nil || fingerprint(a) != source[name] {
			return result, fmt.Errorf("authoring content changed")
		}
		b, err := snapshot(filepath.Join(copyRoot, name))
		if err != nil || fingerprint(b) != actual[name] {
			return result, fmt.Errorf("copy content changed")
		}
	}
	digest := func(v any) string {
		data, _ := json.Marshal(v)
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	result.SourceDigest, result.CopyDigest = digest(source), digest(actual)
	result.Matches = len(result.Missing)+len(result.Changed)+len(result.Unexpected) == 0
	return result, nil
}

func sameStringList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
