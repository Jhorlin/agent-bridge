package bridge

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

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
	if strings.TrimSpace(body) == "" || strings.ContainsAny(body, "$\x00") || strings.Contains(body, "!`") {
		return nil, fmt.Errorf("plugin command arguments, variable expansion and shell preprocessing are unsupported")
	}
	result, err := encodeAgentDocument("claude", fields, body)
	if err == nil {
		result.Mode = raw.Mode
	}
	return result, err
}
