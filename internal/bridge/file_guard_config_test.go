package bridge

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func fileGuardFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "guard", Kind: "file-guard-config", Scope: "project", Portable: true, AllowReformat: true, Claude: ".claude/settings.json", Codex: ".codex/hooks.json", FileGuard: &FileGuardConfig{BridgeExecutable: f.path("bridge"), Scripts: []string{f.path("policy-one"), f.path("policy-two"), f.path("policy-three")}}}}
	f.load()
	f.write(".claude/settings.json", `{"permissions":{"deny":["Bash(rm *)"]},"hooks":{"PreToolUse":[{"matcher":"Glob|Grep","hooks":[{"type":"command","command":"echo LOCAL_ONLY"}]},{"matcher":"Edit|Write|NotebookEdit","hooks":[{"type":"command","command":"\"$CLAUDE_PROJECT_DIR\"/policy-one"}]}]}}`)
	return f
}

func TestFileGuardConfigRoundTripAndLocalOverlays(t *testing.T) {
	f := fileGuardFixture(t)
	f.write(".codex/hooks.json", `{"description":"CODEX_ONLY","hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo KEEP_ME"}]}]}}`)
	original := f.read(".claude/settings.json")
	f.apply()
	f.expect(".claude/settings.json", original)
	if strings.Contains(f.read(".codex/hooks.json"), "LOCAL_ONLY") || !strings.Contains(f.read(".codex/hooks.json"), "KEEP_ME") {
		t.Fatal("local hooks leaked or were lost")
	}
	f.write(".claude/settings.json", strings.Replace(original, "policy-one", "policy-two", 1))
	f.apply()
	if !strings.Contains(f.read(".codex/hooks.json"), "policy-two") {
		t.Fatal("forward update lost")
	}
	f.write(".codex/hooks.json", strings.Replace(f.read(".codex/hooks.json"), "policy-two", "policy-three", 1))
	f.apply()
	if !strings.Contains(f.read(".claude/settings.json"), "policy-three") || !strings.Contains(f.read(".claude/settings.json"), "LOCAL_ONLY") || strings.Contains(f.read(".claude/settings.json"), "timeout") {
		t.Fatal("reverse edit or local settings lost")
	}
	before := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("converged guard wrote files")
	}
	if Audit(f.c).Blocked() {
		t.Fatal("valid guard blocked")
	}
}

func TestFileGuardConfigConflictRecoveryAndResolution(t *testing.T) {
	f := fileGuardFixture(t)
	f.apply()
	original := f.read(".codex/hooks.json")
	f.write(".claude/settings.json", strings.Replace(f.read(".claude/settings.json"), "policy-one", "policy-two", 1))
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	if err == nil {
		t.Fatal("injected error not returned")
	}
	f.expect(".codex/hooks.json", original)
	f.missing("state/pending.json")
	f.write(".codex/hooks.json", strings.Replace(original, "policy-one", "policy-three", 1))
	if !f.plan().HasConflicts() {
		t.Fatal("conflict lost")
	}
	choices := map[string]string{"guard": "claude"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read(".codex/hooks.json"), "policy-two") {
		t.Fatal("resolution did not render guard")
	}
}

func TestFileGuardConfigRejectsDriftWithoutWrites(t *testing.T) {
	for _, mode := range []string{"unknown-script", "codex-command", "codex-timeout", "missing-timeout", "matcher", "event", "async", "duplicate", "shell", "shared-field", "shared-script", "delete"} {
		t.Run(mode, func(t *testing.T) {
			f := fileGuardFixture(t)
			f.apply()
			claude, codex := f.read(".claude/settings.json"), f.read(".codex/hooks.json")
			switch mode {
			case "unknown-script":
				f.write(".claude/settings.json", strings.Replace(claude, "policy-one", "not-reviewed", 1))
			case "codex-command":
				f.write(".codex/hooks.json", strings.Replace(codex, "hook-file-guard", "hook-file-guard --unknown", 1))
			case "codex-timeout", "missing-timeout":
				doc, err := document("claude", f.plan().Items[0].Values["codex"])
				must(t, err)
				s, err := selectFileGuard(f.c.Resources[0], "codex", doc)
				must(t, err)
				if mode == "missing-timeout" {
					delete(s.handler, "timeout")
				} else {
					s.handler["timeout"] = 1
				}
				data, err := json.Marshal(doc)
				must(t, err)
				f.write(".codex/hooks.json", string(data))
			case "matcher":
				f.write(".claude/settings.json", strings.Replace(claude, "Edit|Write|NotebookEdit", "Edit", 1))
			case "event":
				f.write(".claude/settings.json", strings.Replace(claude, "PreToolUse", "PostToolUse", 1))
			case "async":
				f.write(".claude/settings.json", strings.Replace(claude, `"command":"\"`, `"async":true,"command":"\"`, 1))
			case "duplicate":
				f.write(".claude/settings.json", strings.Replace(claude, "echo LOCAL_ONLY", `\"$CLAUDE_PROJECT_DIR\"/policy-one`, 1))
			case "shell":
				f.write(".claude/settings.json", strings.Replace(claude, "policy-one", "policy-one; exit 0", 1))
			case "shared-field":
				f.write("state/shared/guard", `{"script":"ignored","other":true}`)
			case "shared-script":
				f.write("state/shared/guard", `{"script":"/not-reviewed"}`)
			case "delete":
				must(t, os.Remove(f.path(".codex/hooks.json")))
			}
			before := auditTree(t, f.dir)
			_, err := Apply(f.c, Options{})
			if err == nil {
				t.Fatal("unsafe drift accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("rejected drift wrote files")
			}
		})
	}
}

func TestFileGuardConfigDependencyProtectionAndIdentity(t *testing.T) {
	f := fileGuardFixture(t)
	f.apply()
	paths, err := DiagnosticProtectedPaths(f.path("config.json"))
	must(t, err)
	for _, dependency := range FileGuardDependencies(f.c.Resources[0]) {
		found := false
		for _, path := range paths {
			found = found || path == dependency
		}
		if !found || allowedTarget(f.c, dependency) {
			t.Fatal("dependency must be protected but never a recovery target")
		}
	}
	flat, err := flatProfile(f.c)
	must(t, err)
	data, err := snapshotBytes(flat)
	must(t, err)
	f.write("flat.json", string(data))
	c, err := LoadConfig(f.path("flat.json"))
	must(t, err)
	if !reflect.DeepEqual(c.Resources, f.c.Resources) {
		t.Fatal("enrollment lost guard metadata")
	}
	f.raw.Resources[0].FileGuard.Scripts = append(f.raw.Resources[0].FileGuard.Scripts, f.path("new-script"))
	f.load()
	_, err = Plan(f.c)
	contains(t, err, "changed identity")
}

func TestFileGuardConfigRejectsUnsafeConfiguration(t *testing.T) {
	for _, mode := range []string{"unreviewed", "kind", "global", "linked", "native-path", "empty", "relative", "outside", "duplicate", "overlap", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := fileGuardFixture(t)
			r := &f.raw.Resources[0]
			switch mode {
			case "unreviewed":
				r.Portable = false
			case "kind":
				r.Kind = "hook-config"
			case "global":
				r.Scope = "global"
			case "linked":
				r.LinkTargets = map[string]string{"claude": f.path("target")}
			case "native-path":
				r.Codex = "elsewhere.json"
			case "empty":
				r.FileGuard.Scripts = nil
			case "relative":
				r.FileGuard.BridgeExecutable = "bridge"
			case "outside":
				r.FileGuard.Scripts[0] = "/outside-policy"
			case "duplicate":
				r.FileGuard.Scripts[1] = r.FileGuard.Scripts[0]
			case "overlap":
				r.FileGuard.Scripts[0] = f.path(".claude/settings.json")
			case "symlink":
				must(t, os.Symlink(f.path("policy-two"), f.path("policy-one")))
			}
			data, err := json.Marshal(f.raw)
			must(t, err)
			f.write("config.json", string(data))
			if _, err := LoadConfig(f.path("config.json")); err == nil {
				t.Fatal("unsafe guard config accepted")
			}
		})
	}
}

func TestFileGuardConfigConventionsAndDependencyClaims(t *testing.T) {
	f := fileGuardFixture(t)
	f.raw.Conventions = &Conventions{Root: ".", Features: []string{"hooks"}}
	f.load()
	if len(f.c.Resources) != 1 || f.c.Resources[0].Kind != "file-guard-config" {
		t.Fatal("explicit selection did not replace conventional whole-hook mapping")
	}
	f.apply()
	f.write("other.json", `{"version":1,"stateDir":"other-state","resources":[{"id":"other","kind":"portable-file","scope":"project","claude":"policy-one","codex":"other-policy"}]}`)
	report, err := CheckOverlaps([]string{f.path("config.json"), f.path("other.json")})
	must(t, err)
	if len(report.Overlaps) != 1 || !strings.HasSuffix(report.Overlaps[0].First.Role, ":file-guard-dependency") {
		t.Fatal("another profile could overwrite the policy executable")
	}
	claim := PathClaim{"one", "first:file-guard-dependency", f.path("bridge")}
	other := PathClaim{"two", "second:file-guard-dependency", f.path("bridge")}
	if len(overlappingClaims([]PathClaim{claim}, []PathClaim{other})) != 0 {
		t.Fatal("shared read-only executable blocked")
	}
}

func TestFileGuardConfigQuotedPathsAndLocalTimeout(t *testing.T) {
	f := fileGuardFixture(t)
	f.raw.Resources[0].FileGuard.BridgeExecutable = f.path("bridge ' dollar$ space")
	// Timeout digits can also occur in a real filename (or a random temp root).
	f.raw.Resources[0].FileGuard.Scripts[0] = f.path("policy60 ' dollar$ space")
	f.load()
	r := f.c.Resources[0]
	doc := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Edit|Write|NotebookEdit", "hooks": []any{map[string]any{"type": "command", "command": guardCommand(r, "claude", r.FileGuard.Scripts[0]), "timeout": 60}}}}}}
	data, err := json.Marshal(doc)
	must(t, err)
	f.write(".claude/settings.json", string(data))
	f.apply()
	doc, err = document("claude", f.plan().Items[0].Values["claude"])
	must(t, err)
	selected, err := selectFileGuard(r, "claude", doc)
	must(t, err)
	selected.handler["timeout"] = 45
	data, err = json.Marshal(doc)
	must(t, err)
	f.write(".claude/settings.json", string(data))
	if len(f.plan().Items[0].Writes) != 0 {
		t.Fatal("host-local timeout entered shared state")
	}
	p := f.plan()
	value, err := normalizeFileGuard(r, "codex", p.Items[0].Values["codex"])
	must(t, err)
	if !equal(value, p.Items[0].Values["shared"]) {
		t.Fatal("quoted command failed roundtrip")
	}
	f.write("bridge ' dollar$ space", "#!/bin/sh\nprintf '%s\\n' \"$@\"\n")
	must(t, os.Chmod(r.FileGuard.BridgeExecutable, 0700))
	out, err := exec.Command("/bin/sh", "-c", guardCommand(r, "codex", r.FileGuard.Scripts[0])).CombinedOutput()
	must(t, err)
	if string(out) != "hook-file-guard\n"+f.dir+"\n"+r.FileGuard.Scripts[0]+"\n" {
		t.Fatal("shell changed quoted arguments")
	}
}
