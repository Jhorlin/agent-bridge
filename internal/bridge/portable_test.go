package bridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func portableFixture(t *testing.T, kind, source string) *fixture {
	f := newFixture(t)
	f.raw.Resources = []resourceInput{{ID: "portable", Kind: kind, Portable: true, Scope: "project", Claude: "claude-source", Codex: "codex-source", AllowReformat: kind != "instruction-file"}}
	f.load()
	f.write("claude-source", source)
	return f
}
func instructionsFixture(t *testing.T) *fixture {
	return portableFixture(t, "instruction-file", "Claude only\n"+instructionStart+"\nShared guidance.\n"+instructionEnd+"\nClaude footer\n")
}
func agentFixture(t *testing.T) *fixture {
	return portableFixture(t, "agent-file", "---\nname: reviewer\ndescription: Review code.\n---\nReview carefully.\n")
}
func hooksFixture(t *testing.T) *fixture {
	return portableFixture(t, "hook-config", `{"permissions":{"deny":["Bash"]},"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"/usr/bin/true","timeout":10}]}]}}`)
}

func promptHooksFixture(t *testing.T) *fixture {
	return portableFixture(t, "hook-config", `{"permissions":{"deny":["Bash"]},"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/usr/bin/true","timeout":10}]}]}}`)
}

func stopHooksFixture(t *testing.T) *fixture {
	f := promptHooksFixture(t)
	f.write("claude-source", strings.ReplaceAll(f.read("claude-source"), "UserPromptSubmit", "Stop"))
	return f
}

func TestInstructionOverlaysBidirectional(t *testing.T) {
	f := instructionsFixture(t)
	f.write("codex-source", "Codex only\n"+instructionStart+"\nShared guidance.\n"+instructionEnd+"\nCodex footer\n")
	f.apply()
	// Different host-only edits are independent of the shared baseline.
	f.write("claude-source", strings.Replace(f.read("claude-source"), "Claude only", "Changed Claude only", 1))
	f.write("codex-source", strings.Replace(f.read("codex-source"), "Shared guidance.", "Edited from Codex.", 1))
	f.apply()
	if !strings.Contains(f.read("claude-source"), "Changed Claude only") || !strings.Contains(f.read("claude-source"), "Edited from Codex.") {
		t.Fatal("lost local overlay or reverse edit")
	}
	if strings.Contains(f.read("codex-source"), "Claude") {
		t.Fatal("host overlay leaked")
	}
	before := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("no-op rewrote files")
	}
	f.write("state/shared/portable", "\nEdit shared store.\n")
	f.apply()
	if !strings.Contains(f.read("codex-source"), "Edit shared store.") {
		t.Fatal("shared edit not propagated")
	}
}

func TestNewPortableAdaptersRoundTripConflictAndRecovery(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) *fixture{"instructions": instructionsFixture, "agent": agentFixture, "hooks": hooksFixture, "prompt-hooks": promptHooksFixture, "stop-hooks": stopHooksFixture} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			before := auditTree(t, f.dir)
			if Audit(f.c).Blocked() {
				t.Fatal("valid resource blocked")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("audit writes")
			}
			f.apply()
			original := f.read("claude-source")
			replacement := strings.NewReplacer("Shared guidance.", "new guidance", "Review carefully.", "new review", "/usr/bin/true", "/usr/bin/false")
			f.write("claude-source", replacement.Replace(original))
			_, err := Apply(f.c, Options{BeforeWrite: failSecond})
			if err == nil {
				t.Fatal("expected injected failure")
			}
			// Exact original target formatting is restored, pending removed.
			f.missing("state/pending.json")
			f.apply()
			f.write("codex-source", strings.NewReplacer("new guidance", "reverse guidance", "new review", "reverse review", "/usr/bin/false", "/usr/bin/yes").Replace(f.read("codex-source")))
			f.apply()
			if !strings.Contains(f.read("claude-source"), "reverse") && !strings.Contains(f.read("claude-source"), "/usr/bin/yes") {
				t.Fatal("reverse edit lost")
			}
			f.write("claude-source", original)
			f.write("codex-source", strings.NewReplacer("reverse guidance", "conflict", "reverse review", "conflict", "/usr/bin/yes", "/bin/true").Replace(f.read("codex-source")))
			if !f.plan().HasConflicts() {
				t.Fatal("concurrent conflict missed")
			}
		})
	}
}

func TestPromptHooksRejectMatchers(t *testing.T) {
	f := promptHooksFixture(t)
	f.write("claude-source", strings.Replace(f.read("claude-source"), `{"hooks":[`, `{"matcher":"*","hooks":[`, 1))
	if !Audit(f.c).Blocked() {
		t.Fatal("ignored prompt matcher accepted")
	}
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("wrote unsupported prompt hook")
	}
	f.missing("codex-source")
}

func TestInstructionMarkersFailClosed(t *testing.T) {
	for _, source := range []string{"no markers", instructionEnd + instructionStart, instructionStart + instructionStart + instructionEnd} {
		f := portableFixture(t, "instruction-file", source)
		if !Audit(f.c).Blocked() {
			t.Fatal("invalid markers accepted")
		}
	}
}

func TestAgentUnknownFieldsAndInvalidInput(t *testing.T) {
	for _, source := range []string{
		"---\nname: reviewer\ndescription: test\nmodel: private-model\n---\nbody",
		"---\nname: reviewer\nname: duplicate\ndescription: test\n---\nbody",
		"---\nname: reviewer\ndescription: test\ntools: Bash\n---\nbody",
		"---\nname: reviewer\ndescription: test\n---\n",
		"missing frontmatter",
	} {
		f := portableFixture(t, "agent-file", source)
		before := auditTree(t, f.dir)
		report := Audit(f.c)
		if !report.Blocked() {
			t.Fatal("invalid agent accepted")
		}
		b, _ := json.Marshal(report)
		if strings.Contains(string(b), "private-model") {
			t.Fatal("agent value leaked")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("audit wrote")
		}
	}
	f := agentFixture(t)
	f.apply()
	f.write("codex-source", f.read("codex-source")+"\nsandbox_mode='danger-full-access'\n")
	if !Audit(f.c).Blocked() {
		t.Fatal("host security field accepted")
	}
}

func TestHookUnsupportedVariantsAndSettingsPreservation(t *testing.T) {
	f := hooksFixture(t)
	f.write("codex-source", `{"description":"local metadata"}`)
	f.apply()
	f.write("codex-source", strings.Replace(f.read("codex-source"), "/usr/bin/true", "/usr/bin/false", 1))
	f.apply()
	if !strings.Contains(f.read("claude-source"), "permissions") || !strings.Contains(f.read("codex-source"), "local metadata") {
		t.Fatal("unrelated settings lost")
	}
	source := hooksFixture(t).read("claude-source")
	for _, change := range [][2]string{
		{"SessionStart", "PreToolUse"}, {"^startup$", "startup|resume"}, {"\"type\":\"command\"", "\"type\":\"prompt\""}, {"/usr/bin/true", "/bin/true; touch /tmp/unsafe"}, {"\"timeout\":10", "\"timeout\":0"}, {"\"timeout\":10", "\"timeout\":1.5"}, {"\"timeout\":10", "\"timeout\":10,\"async\":true"},
	} {
		f := portableFixture(t, "hook-config", strings.Replace(source, change[0], change[1], 1))
		before := auditTree(t, f.dir)
		if !Audit(f.c).Blocked() {
			t.Fatalf("accepted unsupported hook %v", change)
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("hook audit wrote files")
		}
	}
}

func TestPortableAdapterConsentRequired(t *testing.T) {
	for _, kind := range []string{"instruction-file", "agent-file", "hook-config"} {
		f := newFixture(t)
		f.raw.Resources[0].Kind = kind
		f.save()
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatal("missing portability consent accepted")
		}
	}
}
