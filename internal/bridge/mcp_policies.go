package bridge

import (
	"fmt"
	"math"
	"strings"
)

var codexMCPPolicyFields = []string{"enabled", "required", "enabled_tools", "disabled_tools", "startup_timeout_sec", "tool_timeout_sec", "default_tools_approval_mode", "tools"}

// Host policies never enter the shared server model. This opt-in retains their
// values only in their existing Codex document, including disabled/empty lists.
func splitCodexMCPPolicies(m map[string]any) (portable, local map[string]any, err error) {
	portable, local = map[string]any{}, map[string]any{}
	for k, v := range m {
		if !hasField(codexMCPPolicyFields, k) {
			portable[k] = v
			continue
		}
		local[k] = v
		switch k {
		case "enabled", "required":
			if _, ok := v.(bool); !ok {
				return nil, nil, fmt.Errorf("MCP local policy requires boolean")
			}
		case "enabled_tools", "disabled_tools":
			values, e := stringArray(v)
			if e != nil || v == nil {
				return nil, nil, fmt.Errorf("MCP local policy requires tool list")
			}
			seen := map[string]bool{}
			for _, name := range values {
				if !policyToolName(name) || seen[name] {
					return nil, nil, fmt.Errorf("invalid local policy tool name")
				}
				seen[name] = true
			}
		case "startup_timeout_sec", "tool_timeout_sec":
			var seconds float64
			switch n := v.(type) {
			case int64:
				seconds = float64(n)
			case float64:
				seconds = n
			default:
				return nil, nil, fmt.Errorf("MCP timeout requires number")
			}
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 86400 {
				return nil, nil, fmt.Errorf("MCP local timeout must be positive and at most one day")
			}
		case "default_tools_approval_mode":
			if !policyApprovalMode(v) {
				return nil, nil, fmt.Errorf("unsupported MCP approval mode")
			}
		case "tools":
			entries, ok := v.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("MCP tool policies require table")
			}
			for name, raw := range entries {
				fields, ok := raw.(map[string]any)
				if !ok || !policyToolName(name) {
					return nil, nil, fmt.Errorf("invalid MCP tool policy")
				}
				for key, value := range fields {
					switch key {
					case "approval_mode":
						if !policyApprovalMode(value) {
							return nil, nil, fmt.Errorf("unsupported MCP approval mode")
						}
					case "output_token_limit":
						n, ok := value.(int64)
						if !ok || n <= 0 {
							return nil, nil, fmt.Errorf("MCP output limit requires positive integer")
						}
					default:
						return nil, nil, fmt.Errorf("unsupported MCP tool policy field")
					}
				}
			}
		}
	}
	return portable, local, nil
}

func policyToolName(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\x00\r\n\t")
}
func policyApprovalMode(v any) bool {
	s, ok := v.(string)
	return ok && (s == "auto" || s == "prompt" || s == "writes" || s == "approve")
}
