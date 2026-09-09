package bridge

import (
	"path/filepath"
	"sort"
)

// Audit rejects unknown profile options rather than silently omitting them.
// Keep the sync loader's existing compatibility contract unchanged.
func LoadAuditConfig(filename string) (Config, error) {
	return loadConfig(filename, true)
}

// Audit reports compatibility, not host execution or a guarantee of future sync.
// Only resource IDs and fixed vocabulary enter diagnostics; never raw errors,
// filenames, server names, unknown keys, or native configuration values.
type AuditReport struct {
	ConventionWarnings []string        `json:"conventionWarnings,omitempty"`
	Version            int             `json:"version"`
	ReadOnly           bool            `json:"readOnly"`
	HostVerified       bool            `json:"hostVerified"`
	Resources          []AuditResource `json:"resources"`
}
type AuditResource struct {
	ID        string       `json:"id"`
	Kind      string       `json:"kind"`
	Scope     string       `json:"scope"`
	Direction string       `json:"direction"`
	Status    string       `json:"status"`
	Checks    []AuditCheck `json:"checks"`
	Actions   []string     `json:"actions"`
}
type AuditCheck struct {
	Side              string   `json:"side"`
	Status            string   `json:"status"`
	RecognizedFields  []string `json:"recognizedFields"`
	UnsupportedFields []string `json:"unsupportedFields"`
	UnknownFields     int      `json:"unknownFields"`
}

func (a AuditReport) Blocked() bool {
	for _, r := range a.Resources {
		if r.Status == "blocked" {
			return true
		}
	}
	return false
}

func Audit(c Config) AuditReport {
	report := AuditReport{Version: 1, ReadOnly: true, Resources: []AuditResource{}}
	report.ConventionWarnings = c.ConventionWarnings
	resources := append([]Resource(nil), c.Resources...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	for _, r := range resources {
		row := AuditResource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Direction: "bidirectional", Status: "review-required", Checks: []AuditCheck{}, Actions: []string{"Run plan before sync; audit is not a write authorization or live host certification."}}
		switch r.Kind {
		case "skill-directory":
			if r.TranslateSkillInvocation {
				row.Actions = append(row.Actions, "Invocation mode maps disable-model-invocation to inverse policy.allow_implicit_invocation. Codex entry and explicit policy sidecar form one reviewed unit; history restores policy too. Common informational metadata is retained; host-specific controls and execution contexts remain unsupported.")
			} else if r.AllowReformat {
				row.Actions = append(row.Actions, "Strict metadata validates name, description and bounded informational fields and preserves instruction bytes; invocation policies and agents/openai.yaml are unsupported. Review body, scripts and host loading separately.")
			} else {
				row.Actions = append(row.Actions, "Raw skill copying does not validate frontmatter or host behavior. Use allowReformat for opt-in strict common metadata validation.")
			}
		case "portable-file", "instruction-file":
			row.Actions = append(row.Actions, "Review instruction, metadata, script and tool compatibility in both hosts; byte copying does not validate behavior.")
		case "mcp-config":
			row.Actions = append(row.Actions, "Authenticate and verify selected tools separately in each host; review formatting loss and host-local policies.")
			if r.PreserveCodexMCPPolicies {
				row.Actions = append(row.Actions, "Codex MCP policies are retained locally, not translated. Configure Claude enablement and tool permissions independently before use.")
			}
		case "plugin-directory":
			row.Actions = append(row.Actions, "Review skill behavior and install or refresh separately in each host; bounded conventional compatibility-layout hooks, commands and allowlisted MCP are bridged. Command names differ after Codex migration; arguments and direct bundled Codex agents remain unsupported.")
			if len(r.CodexAgentExports) > 0 {
				row.Actions = append(row.Actions, "Explicit plugin agents export as namespaced standalone Codex files, not installed package components. Exports remain separately available after plugin uninstall; permissions, models, agent scheduling and arbitrary body behavior are not translated.")
				if r.PreserveAgentSettings {
					row.Actions = append(row.Actions, "Bounded agent settings remain in each original host file; configure each export's model and permissions independently before use.")
				}
			}
		case "agent-file":
			row.Actions = append(row.Actions, "Only name, description and instructions are mapped; review native agent discovery and host permissions separately.")
			if r.PreserveAgentSettings {
				row.Actions = append(row.Actions, "Bounded model and permission settings stay in their original host file. Configure each host independently; retained settings are not equivalent cross-host permissions.")
			}
		case "hook-config":
			row.Actions = append(row.Actions, "Startup SessionStart, UserPromptSubmit, Stop and exact-Bash PreToolUse/PostToolUse definitions are mapped. Review and trust hooks in each host; failures can continue tool execution and arbitrary output policy is not translated.")
		}
		// Reuse the actual planner so audit cannot call a rejected resource compatible.
		// Per-resource plans collect independent failures without writing a lock or state.
		local := c
		local.Resources = []Resource{r}
		plan, err := Plan(local)
		if err != nil {
			row.Status = "blocked"
			row.Actions = append(row.Actions, "Resolve invalid or unsupported content, unsafe paths, resource identity changes, or pending/invalid state before syncing.")
		} else if plan.HasConflicts() {
			row.Status = "blocked"
			row.Actions = append(row.Actions, "Reconcile missing sources, deletions or differing edits manually; sync will not choose a winner.")
		}
		if r.Kind == "mcp-config" || r.Kind == "plugin-directory" {
			for _, side := range sides[1:] {
				check := auditNativeFields(r, side)
				row.Checks = append(row.Checks, check)
				if check.Status == "blocked" {
					row.Status = "blocked"
				}
			}
		}
		report.Resources = append(report.Resources, row)
	}
	return report
}

func auditNativeFields(r Resource, side string) AuditCheck {
	check := AuditCheck{Side: side, Status: "absent", RecognizedFields: []string{}, UnsupportedFields: []string{}}
	path := r.Paths[side]
	if r.Kind == "plugin-directory" {
		path = filepath.Join(path, pluginManifestFor(r, side))
	}
	raw, err := snapshot(path)
	if err != nil {
		check.Status = "blocked"
		return check
	}
	if raw == nil {
		return check
	}
	syntax := side
	if r.Kind == "plugin-directory" {
		syntax = "claude"
	}
	doc, err := document(syntax, raw)
	if err != nil {
		check.Status = "blocked"
		return check
	}
	check.Status = "recognized-fields-only"
	recognized, unsupported := map[string]bool{}, map[string]bool{}
	inspect := func(fields map[string]any, allowed, knownUnsupported []string) {
		for key := range fields {
			if hasField(allowed, key) {
				recognized[key] = true
			} else if hasField(knownUnsupported, key) {
				unsupported[key] = true
			} else {
				check.UnknownFields++
			}
		}
	}
	if r.Kind == "plugin-directory" {
		inspect(doc, []string{"$schema", "name", "version", "description", "author", "homepage", "repository", "license", "keywords", "skills"}, []string{"hooks", "mcpServers", "agents", "commands", "apps", "interface", "extensions", "settings"})
		if _, err := normalizePluginManifest(raw); err != nil {
			check.Status = "blocked"
		}
	} else {
		root := "mcpServers"
		allowed := []string{"command", "args", "cwd", "type", "url", "env", "headers"}
		if side == "codex" {
			root = "mcp_servers"
			allowed = []string{"command", "args", "cwd", "url", "env_vars", "env", "env_http_headers", "bearer_token_env_var"}
			if r.PreserveCodexMCPPolicies {
				allowed = append(allowed, codexMCPPolicyFields...)
			}
		}
		if entries, ok := doc[root].(map[string]any); ok {
			for _, name := range r.Servers {
				if fields, ok := entries[name].(map[string]any); ok {
					inspect(fields, allowed, []string{"enabled", "enabled_tools", "disabled_tools", "startup_timeout_sec", "tool_timeout_sec", "experimental_environment", "http_headers"})
				}
			}
		}
		if _, err := normalizeMCP(r, side, raw); err != nil {
			check.Status = "blocked"
		}
	}
	for key := range recognized {
		check.RecognizedFields = append(check.RecognizedFields, key)
	}
	for key := range unsupported {
		check.UnsupportedFields = append(check.UnsupportedFields, key)
	}
	sort.Strings(check.RecognizedFields)
	sort.Strings(check.UnsupportedFields)
	if len(unsupported) > 0 || check.UnknownFields > 0 {
		check.Status = "blocked"
	}
	return check
}
func hasField(fields []string, key string) bool {
	for _, field := range fields {
		if field == key {
			return true
		}
	}
	return false
}
