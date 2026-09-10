package bridge

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var pluginCommandSegment = regexp.MustCompile(`^[a-z0-9]+([-_][a-z0-9]+)*$`)
var pluginCommandDynamic = regexp.MustCompile(`\$ARGUMENTS|\$[0-9]|\$\{?CLAUDE_`)

// Codex flattens command directories and converts underscores to hyphens.
// Return that native migration identity so collisions cannot silently hide
// commands. This is not a rename of authoring files.
func pluginCommandMigrationName(file string) (string, bool) {
	if !strings.HasPrefix(file, "commands/") || !strings.HasSuffix(file, ".md") {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(file, "commands/"), ".md")
	if len(name) > 64 {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if !pluginCommandSegment.MatchString(part) {
			return "", false
		}
	}
	return "source-command-" + strings.NewReplacer("/", "-", "_", "-").Replace(name), true
}

// Conventional command files are consumed directly by Claude and migrated to
// cache-owned skills by Codex. Keep those installed/generated files out of sync.
func normalizePluginCommand(raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	fields, body, err := agentDocument("claude", raw)
	if err != nil {
		return nil, fmt.Errorf("plugin command requires strict YAML frontmatter")
	}
	description, ok := fields["description"].(string)
	if len(fields) != 1 || !ok || strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 || strings.ContainsAny(description, "\r\n\x00") {
		return nil, fmt.Errorf("plugin commands support only a single-line description field")
	}
	if strings.TrimSpace(body) == "" || strings.ContainsRune(body, '\x00') || pluginCommandDynamic.MatchString(body) || strings.Contains(body, "!`") || strings.Contains(body, "```!") {
		return nil, fmt.Errorf("plugin command arguments, variable expansion and shell preprocessing are unsupported")
	}
	result, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		result.Mode = raw.Mode
	}
	return result, err
}
