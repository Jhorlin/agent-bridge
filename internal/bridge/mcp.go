package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// This is a deliberately bounded common model, not a bag of unchecked options.
type MCPServer struct {
	Transport  string            `json:"transport"`
	Command    string            `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	CWD        string            `json:"cwd,omitempty"`
	EnvVars    []string          `json:"envVars,omitempty"`
	URL        string            `json:"url,omitempty"`
	HeaderVars map[string]string `json:"headerVars,omitempty"`
	BearerEnv  string            `json:"bearerEnv,omitempty"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var envRef = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
var bearerRef = regexp.MustCompile(`^Bearer \$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	// Detect duplicate keys recursively before decoding into the destination.
	var consume func() error
	consume = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate JSON key")
				}
				seen[s] = true
				if err = consume(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err = consume(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := consume(); err != nil {
		return fmt.Errorf("invalid or duplicate-key JSON")
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON document")
	}
	return nil
}
func document(side string, s *Snapshot) (map[string]any, error) {
	result := map[string]any{}
	if s == nil {
		return result, nil
	}
	data, err := base64.StdEncoding.DecodeString(s.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid snapshot")
	}
	if side == "codex" {
		if err = toml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("invalid Codex TOML document")
		}
	} else if err = strictJSON(data, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("expected configuration object")
	}
	return result, nil
}
func textField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("MCP field requires string")
	}
	return s, nil
}
func stringArray(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("MCP field requires string array")
	}
	out := []string{}
	for _, x := range a {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("MCP field requires string array")
		}
		out = append(out, s)
	}
	return out, nil
}
func stringMap(v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("MCP field requires string map")
	}
	out := map[string]string{}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("MCP field requires string map")
		}
		out[k] = s
	}
	return out, nil
}
func noExpansion(s string) bool { return !strings.Contains(s, "${") && !strings.Contains(s, "${env:") }

func serverFromNative(side string, m map[string]any) (MCPServer, error) {
	s := MCPServer{Transport: "stdio"}
	allowed := map[string]bool{"command": true, "args": true, "cwd": true}
	if side == "claude" {
		for _, k := range []string{"type", "url", "env", "headers"} {
			allowed[k] = true
		}
	} else {
		for _, k := range []string{"url", "env_vars", "env", "env_http_headers", "bearer_token_env_var"} {
			allowed[k] = true
		}
	}
	for k := range m {
		if !allowed[k] {
			return s, fmt.Errorf("unsupported MCP option; keep tool-specific policies outside this bridge")
		}
	}
	var err error
	if s.Command, err = textField(m, "command"); err != nil {
		return s, err
	}
	if s.URL, err = textField(m, "url"); err != nil {
		return s, err
	}
	if s.CWD, err = textField(m, "cwd"); err != nil {
		return s, err
	}
	if s.Args, err = stringArray(m["args"]); err != nil {
		return s, err
	}
	if s.URL != "" {
		s.Transport = "http"
	}
	if side == "claude" {
		typ, err := textField(m, "type")
		if err != nil {
			return s, err
		}
		if typ != "" && typ != s.Transport {
			return s, fmt.Errorf("unsupported or inconsistent MCP transport")
		}
	}
	env, err := stringMap(m["env"])
	if err != nil {
		return s, err
	}
	if side == "claude" {
		for k, v := range env {
			match := envRef.FindStringSubmatch(v)
			if len(match) != 2 || match[1] != k {
				return s, fmt.Errorf("MCP env requires same-name ${VAR} references; literals and renaming are unsupported")
			}
			s.EnvVars = append(s.EnvVars, k)
		}
	} else {
		if len(env) > 0 {
			return s, fmt.Errorf("literal Codex MCP env is unsupported; use env_vars")
		}
		if s.EnvVars, err = stringArray(m["env_vars"]); err != nil {
			return s, err
		}
	}
	if side == "claude" {
		headers, err := stringMap(m["headers"])
		if err != nil {
			return s, err
		}
		s.HeaderVars = map[string]string{}
		seenHeaders := map[string]bool{}
		for k, v := range headers {
			fold := strings.ToLower(k)
			if seenHeaders[fold] {
				return s, fmt.Errorf("duplicate case-insensitive MCP header")
			}
			seenHeaders[fold] = true
			if strings.EqualFold(k, "Authorization") {
				if match := bearerRef.FindStringSubmatch(v); len(match) == 2 {
					s.BearerEnv = match[1]
					continue
				}
			}
			match := envRef.FindStringSubmatch(v)
			if len(match) != 2 {
				return s, fmt.Errorf("MCP headers require ${VAR} references; literal credentials are unsupported")
			}
			s.HeaderVars[k] = match[1]
		}
	} else {
		if s.HeaderVars, err = stringMap(m["env_http_headers"]); err != nil {
			return s, err
		}
		if s.BearerEnv, err = textField(m, "bearer_token_env_var"); err != nil {
			return s, err
		}
	}
	return canonicalServer(s)
}
func canonicalServer(s MCPServer) (MCPServer, error) {
	// A bounded check for explicit credential-bearing command options. Never
	// copy their values into the canonical store, logs, or opposite host. This
	// intentionally is not a general secret scanner for arbitrary script text.
	for _, arg := range s.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		flag := strings.SplitN(arg, "=", 2)[0]
		flag = strings.ReplaceAll(strings.ToLower(strings.TrimLeft(flag, "-")), "_", "-")
		for _, sensitive := range []string{"password", "passwd", "secret", "token", "api-key", "apikey", "authorization"} {
			if flag == sensitive || strings.HasSuffix(flag, "-"+sensitive) {
				return s, fmt.Errorf("MCP credential-bearing command arguments require host-local secret setup; values are not portable")
			}
		}
	}
	if s.CWD != "" && !filepath.IsAbs(s.CWD) {
		return s, fmt.Errorf("MCP cwd must be absolute to retain meaning across scopes")
	}
	if s.Transport != "stdio" && s.Transport != "http" {
		return s, fmt.Errorf("unsupported MCP transport")
	}
	if s.Transport == "stdio" {
		if s.Command == "" || s.URL != "" || len(s.HeaderVars) > 0 || s.BearerEnv != "" {
			return s, fmt.Errorf("invalid stdio MCP fields")
		}
	} else {
		if s.Command != "" || len(s.Args) > 0 || s.CWD != "" || len(s.EnvVars) > 0 {
			return s, fmt.Errorf("invalid HTTP MCP fields")
		}
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return s, fmt.Errorf("MCP URL must be HTTP(S) without credentials, query, or fragment")
		}
	}
	for _, v := range append([]string{s.Command, s.CWD, s.URL}, s.Args...) {
		if !noExpansion(v) {
			return s, fmt.Errorf("MCP interpolation in commands, args, cwd, or URL cannot be translated safely")
		}
	}
	seen := map[string]bool{}
	for _, v := range s.EnvVars {
		if !envName.MatchString(v) || seen[v] {
			return s, fmt.Errorf("invalid or duplicate environment variable name")
		}
		seen[v] = true
	}
	sort.Strings(s.EnvVars)
	if s.BearerEnv != "" && !envName.MatchString(s.BearerEnv) {
		return s, fmt.Errorf("invalid bearer environment variable name")
	}
	headers := map[string]bool{}
	for k, v := range s.HeaderVars {
		fold := strings.ToLower(k)
		if k == "" || strings.ContainsAny(k, "\r\n") || !envName.MatchString(v) || headers[fold] || (fold == "authorization" && s.BearerEnv != "") {
			return s, fmt.Errorf("invalid or conflicting header reference")
		}
		headers[fold] = true
	}
	return s, nil
}
func normalizeMCP(r Resource, side string, raw *Snapshot) (*Snapshot, error) {
	doc, err := document(side, raw)
	if err != nil {
		return nil, err
	}
	entries := doc
	if side != "shared" {
		key := "mcpServers"
		if side == "codex" {
			key = "mcp_servers"
		}
		if v, ok := doc[key]; ok {
			entries, ok = v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("MCP servers must be a table/object")
			}
		} else {
			entries = map[string]any{}
		}
	}
	normalized := map[string]MCPServer{}
	for _, name := range r.Servers {
		v, ok := entries[name]
		if !ok {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP server must be an object")
		}
		var s MCPServer
		if side == "shared" {
			data, _ := json.Marshal(m)
			d := json.NewDecoder(bytes.NewReader(data))
			d.DisallowUnknownFields()
			if err = d.Decode(&s); err != nil {
				return nil, fmt.Errorf("invalid shared MCP server")
			}
			s, err = canonicalServer(s)
		} else {
			if side == "codex" && r.PreserveCodexMCPPolicies {
				m, _, err = splitCodexMCPPolicies(m)
				if err != nil {
					return nil, err
				}
			}
			s, err = serverFromNative(side, m)
		}
		if err != nil {
			return nil, fmt.Errorf("MCP resource %s: %w", r.ID, err)
		}
		normalized[name] = s
	}
	if len(normalized) == 0 && !featureResourceID(r.ID) {
		return nil, nil
	}
	if len(normalized) != len(r.Servers) && !featureResourceID(r.ID) {
		return nil, fmt.Errorf("partial MCP allowlist: every selected server must be present or all absent")
	}
	if side == "shared" && len(entries) != len(normalized) {
		return nil, fmt.Errorf("shared MCP contains unregistered servers")
	}
	return encoded(normalized)
}
func nativeServer(side string, s MCPServer) map[string]any {
	out := map[string]any{}
	if s.Transport == "stdio" {
		out["command"] = s.Command
		if len(s.Args) > 0 {
			out["args"] = s.Args
		}
		if s.CWD != "" {
			out["cwd"] = s.CWD
		}
		if len(s.EnvVars) > 0 {
			if side == "claude" {
				m := map[string]string{}
				for _, v := range s.EnvVars {
					m[v] = "${" + v + "}"
				}
				out["env"] = m
			} else {
				out["env_vars"] = s.EnvVars
			}
		}
	} else {
		out["url"] = s.URL
		if side == "claude" {
			out["type"] = "http"
			headers := map[string]string{}
			for k, v := range s.HeaderVars {
				headers[k] = "${" + v + "}"
			}
			if s.BearerEnv != "" {
				headers["Authorization"] = "Bearer ${" + s.BearerEnv + "}"
			}
			if len(headers) > 0 {
				out["headers"] = headers
			}
		} else {
			if len(s.HeaderVars) > 0 {
				out["env_http_headers"] = s.HeaderVars
			}
			if s.BearerEnv != "" {
				out["bearer_token_env_var"] = s.BearerEnv
			}
		}
	}
	return out
}
func renderMCP(r Resource, side string, content, before *Snapshot) (*Snapshot, error) {
	if side == "shared" {
		return content, nil
	}
	var servers map[string]MCPServer
	if err := decode(content, &servers); err != nil {
		return nil, err
	}
	doc, err := document(side, before)
	if err != nil {
		return nil, err
	}
	key := "mcpServers"
	if side == "codex" {
		key = "mcp_servers"
	}
	entries := map[string]any{}
	if existing, ok := doc[key]; ok {
		entries, ok = existing.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP servers must be an object")
		}
	}
	for _, name := range r.Servers {
		server, ok := servers[name]
		if !ok {
			return nil, fmt.Errorf("selected MCP server cannot be deleted")
		}
		output := nativeServer(side, server)
		if side == "codex" && r.PreserveCodexMCPPolicies {
			if existing, ok := entries[name]; ok {
				fields, ok := existing.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("MCP server must be an object")
				}
				_, policies, err := splitCodexMCPPolicies(fields)
				if err != nil {
					return nil, err
				}
				for key, value := range policies {
					output[key] = value
				}
			}
		}
		entries[name] = output
	}
	doc[key] = entries
	if preserved := preserveMCPText(side, before, doc); preserved != nil {
		return preserved, nil
	}
	if side == "claude" {
		return encoded(doc)
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("cannot encode Codex TOML")
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString(data), Mode: 0600}, nil
}
