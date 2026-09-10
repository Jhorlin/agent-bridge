package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const instructionStart = "<!-- agent-bridge:shared:start -->"
const instructionEnd = "<!-- agent-bridge:shared:end -->"

func snapshotBytes(s *Snapshot) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s.Data)
}

// Only the explicit common block participates in reconciliation. Native text
// outside that block remains owned by its host and is preserved exactly.
func instructionParts(raw *Snapshot) (string, string, string, error) {
	data, err := snapshotBytes(raw)
	if err != nil {
		return "", "", "", err
	}
	if raw == nil {
		return "", "", "", nil
	}
	text := string(data)
	if strings.Count(text, instructionStart) != 1 || strings.Count(text, instructionEnd) != 1 {
		return "", "", "", fmt.Errorf("instructions require exactly one shared start/end marker pair")
	}
	start := strings.Index(text, instructionStart) + len(instructionStart)
	end := strings.Index(text, instructionEnd)
	if end < start {
		return "", "", "", fmt.Errorf("instruction markers are out of order")
	}
	return text[:start], text[start:end], text[end:], nil
}
func normalizeInstructions(side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil || side == "shared" {
		return raw, nil
	}
	_, body, _, err := instructionParts(raw)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(body)), Mode: raw.Mode}, nil
}
func renderInstructions(side string, content, before *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	prefix, _, suffix, err := instructionParts(before)
	if err != nil {
		return nil, err
	}
	if before == nil {
		prefix = instructionStart
		suffix = instructionEnd + "\n"
	}
	body, err := snapshotBytes(content)
	if err != nil {
		return nil, err
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(prefix + string(body) + suffix)), Mode: content.Mode}, nil
}

type portableAgent struct {
	Name         string `json:"name" yaml:"name" toml:"name"`
	Description  string `json:"description" yaml:"description" toml:"description"`
	Instructions string `json:"developer_instructions" yaml:"-" toml:"developer_instructions"`
}

func normalizeAgent(side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := snapshotBytes(raw)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("agent definitions must be valid UTF-8")
	}
	var agent portableAgent
	switch side {
	case "claude":
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.HasPrefix(text, "---\n") {
			return nil, fmt.Errorf("agent requires YAML frontmatter")
		}
		parts := strings.SplitN(text[4:], "\n---\n", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("agent frontmatter is unterminated")
		}
		d := yaml.NewDecoder(strings.NewReader(parts[0]))
		d.KnownFields(true)
		if err := d.Decode(&agent); err != nil {
			return nil, fmt.Errorf("invalid or unsupported agent frontmatter; only name and description are portable")
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("multiple agent frontmatter documents are unsupported")
		}
		agent.Instructions = parts[1]
	case "codex":
		d := toml.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(&agent); err != nil {
			return nil, fmt.Errorf("invalid or unsupported agent TOML; keep models, permissions and tools host-local")
		}
	default:
		if err := strictJSON(data, &agent); err != nil {
			return nil, err
		}
		var fields map[string]any
		if err := strictJSON(data, &fields); err != nil {
			return nil, err
		}
		for k := range fields {
			if k != "name" && k != "description" && k != "developer_instructions" {
				return nil, fmt.Errorf("unsupported shared agent field")
			}
		}
	}
	if !utf8.ValidString(agent.Name) || !utf8.ValidString(agent.Description) || !utf8.ValidString(agent.Instructions) {
		return nil, fmt.Errorf("decoded agent fields must be valid UTF-8")
	}
	if !safeID.MatchString(agent.Name) || strings.TrimSpace(agent.Description) == "" || strings.TrimSpace(agent.Instructions) == "" {
		return nil, fmt.Errorf("agent name, description and instructions are required")
	}
	return encoded(agent)
}
func renderAgent(side string, content *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	var agent portableAgent
	if err := decode(content, &agent); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	if side == "claude" {
		var front []byte
		front, err = yaml.Marshal(struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
		}{agent.Name, agent.Description})
		data = []byte("---\n" + string(front) + "---\n" + agent.Instructions)
	} else {
		data, err = toml.Marshal(agent)
	}
	if err != nil {
		return nil, fmt.Errorf("agent rendering failed")
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString(data), Mode: content.Mode}, nil
}

var hookExecutable = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

// Bounded hook contract: synchronous, explicitly timed startup SessionStart and
// UserPromptSubmit/Stop commands and exact Bash pre/post tool events. No shell
// expressions or prompt handlers; output policy remains the host's responsibility.
func normalizeHooks(side string, raw *Snapshot) (*Snapshot, error) {
	return normalizeHooksForResource(Resource{}, side, raw)
}

func normalizeHooksForResource(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	doc, err := document("claude", raw)
	if err != nil {
		return nil, err
	}
	hooks := doc
	if side != "shared" {
		value, ok := doc["hooks"]
		if !ok {
			return nil, nil
		}
		hooks, ok = value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hooks must be an object")
		}
	}
	if len(hooks) == 0 {
		return nil, nil
	}
	for event, groups := range hooks {
		if event != "SessionStart" && event != "UserPromptSubmit" && event != "Stop" && event != "PreToolUse" && event != "PostToolUse" {
			return nil, fmt.Errorf("only startup, prompt, stop and Bash pre/post tool hooks are mapped in this version")
		}
		list, ok := groups.([]any)
		if !ok || len(list) == 0 {
			return nil, fmt.Errorf("hook groups must be nonempty")
		}
		for _, value := range list {
			group, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid hook group")
			}
			for key := range group {
				if key != "matcher" && key != "hooks" {
					return nil, fmt.Errorf("unsupported hook group field")
				}
			}
			if event == "SessionStart" && group["matcher"] != "^startup$" {
				return nil, fmt.Errorf("portable hooks require exact startup matcher")
			}
			if (event == "PreToolUse" || event == "PostToolUse") && group["matcher"] != "^Bash$" {
				return nil, fmt.Errorf("tool hooks require the exact ^Bash$ matcher")
			}
			if event == "UserPromptSubmit" || event == "Stop" {
				if _, ok := group["matcher"]; ok {
					return nil, fmt.Errorf("prompt and stop hooks do not support a matcher")
				}
			}
			handlers, ok := group["hooks"].([]any)
			if !ok || len(handlers) == 0 {
				return nil, fmt.Errorf("hook handlers must be nonempty")
			}
			for _, value := range handlers {
				h, ok := value.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid hook handler")
				}
				for key := range h {
					if key != "type" && key != "command" && key != "timeout" {
						return nil, fmt.Errorf("unsupported hook handler field")
					}
				}
				command, ok := h["command"].(string)
				validCommand := hookExecutable.MatchString(command) && filepath.Clean(command) == command
				if r.Kind == "plugin-directory" {
					_, relative := pluginHookDependencies(command)
					validCommand = validCommand || relative
				}
				if h["type"] != "command" || !ok || !validCommand {
					return nil, fmt.Errorf("hook command must be an absolute executable path without shell syntax")
				}
				// Materialize source-native defaults, not the destination's.
				// Claude prompt hooks default to 30s; Codex uses 600s. The
				// remaining supported command events default to 600s in both.
				// Shared canonical data has no host and must remain explicit.
				if _, present := h["timeout"]; !present && (side == "claude" || side == "codex") {
					h["timeout"] = json.Number("600")
					if side == "claude" && event == "UserPromptSubmit" {
						h["timeout"] = json.Number("30")
					}
				}
				// JSON numbers retain precision; never coerce nulls or strings.
				value, ok := h["timeout"].(json.Number)
				seconds, err := value.Int64()
				if !ok || err != nil || seconds < 1 || seconds > 600 {
					return nil, fmt.Errorf("hook timeout must be an integer from 1 to 600 seconds")
				}
			}
		}
	}
	return encoded(hooks)
}
func renderHooks(side string, content, before *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	doc, err := document("claude", before)
	if err != nil {
		return nil, err
	}
	var hooks map[string]any
	if err := decode(content, &hooks); err != nil {
		return nil, err
	}
	doc["hooks"] = hooks
	return encoded(doc)
}
