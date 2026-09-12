package bridge

import (
	"reflect"
	"strings"
	"testing"
)

// These inert fixtures reproduce onboarding failures without embedding private
// repository content. Relaxing metadata validation must not turn a malformed
// source into a silently rewritten destination.
func TestMonorepoSkillMetadataRepairAndRoundTrip(t *testing.T) {
	for _, tc := range []struct{ name, invalid, repaired string }{
		{"unquoted-colon", "description: Work in both environments: local and hosted.", "description: 'Work in both environments: local and hosted.'"},
		{"overlong-folded", "description: >\n  " + strings.Repeat("x", 1227), "description: >-\n  Use when migrating a renamed integration."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := invocationFixture(t)
			f.raw.Resources[0].PreserveSkillSettings = true
			f.load()
			const body = "Keep the complete workflow here.\n"
			entry := func(front string) string {
				return "---\nname: demo\n" + front + "\nargument-hint: '[target]'\nallowed-tools: Read\n---\n" + body
			}
			f.write("claude-skill/SKILL.md", entry(tc.invalid))
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("invalid metadata was silently accepted")
			}
			f.missing("codex-skill/SKILL.md")
			f.missing("state/pending.json")
			f.write("claude-skill/SKILL.md", entry(tc.repaired))
			f.apply()
			f.expect("claude-skill/SKILL.md", entry(tc.repaired))
			if !strings.HasSuffix(f.read("codex-skill/SKILL.md"), body) || strings.Contains(f.read("codex-skill/SKILL.md"), "allowed-tools") {
				t.Fatal("workflow lost or host-local grant copied")
			}
			f.write("codex-skill/SKILL.md", strings.Replace(f.read("codex-skill/SKILL.md"), body, "Updated workflow.\n", 1))
			f.apply()
			if !strings.Contains(f.read("claude-skill/SKILL.md"), "Updated workflow.") || !strings.Contains(f.read("claude-skill/SKILL.md"), "allowed-tools: Read") {
				t.Fatal("reverse edit lost workflow or Claude-local metadata")
			}
			for _, item := range f.plan().Items {
				if len(item.Writes) != 0 {
					t.Fatal("repair did not converge")
				}
			}
		})
	}
}

func TestMonorepoReviewedExceptionsPreserveNativeSkillsAndBranches(t *testing.T) {
	for _, branch := range []string{"root-checkout", "worktree-checkout"} {
		t.Run(branch, func(t *testing.T) {
			f := allConventionFixture(t, "project")
			f.raw.Conventions = &Conventions{Root: ".", Scope: "project", Features: []string{"instructions", "skills"}, PreserveSkillSettings: true, Exclude: []string{".agents/skills/VENDORED.md", ".agents/skills/native-demo", ".claude/skills/dynamic-demo"}}
			f.raw.Resources = []resourceInput{
				{ID: "reviewed-root", Kind: "portable-file", Scope: "project", Portable: true, Claude: "CLAUDE.md", Codex: "AGENTS.md"},
				{ID: "reviewed-catalog", Kind: "skill-directory", Scope: "project", Portable: true, Claude: ".claude/skills/catalog", Codex: ".agents/skills/catalog", AllowReformat: true, TranslateSkillInvocation: true, PreserveSkillSettings: true},
			}
			root := branch + " uses @example/codegen.\n" + strings.Repeat("Instruction fixture.\n", 2400)
			f.write("CLAUDE.md", root)
			f.write("packages/component/CLAUDE.md", branch+" nested instructions.\n")
			f.write(".agents/skills/VENDORED.md", "Owned by the native installer.\n")
			f.write(".agents/skills/native-demo/SKILL.md", "Do not adopt this native variant.\n")
			f.write(".claude/skills/dynamic-demo/SKILL.md", "---\nname: dynamic-demo\ndescription: Dynamic fixture.\n---\nUse $ARGUMENTS.\n")
			const catalog = "---\nname: catalog\ndescription: Reviewed catalog fixture.\n---\nA literal price is $2.50; the GraphQL type is `Float!`.\n"
			f.write(".claude/skills/catalog/SKILL.md", catalog)
			f.write(".claude/skills/catalog/references/details.txt", branch)
			f.load()
			f.apply()
			f.expect("AGENTS.md", root)
			f.expect("packages/component/AGENTS.md", branch+" nested instructions.\n")
			f.expect(".claude/skills/catalog/SKILL.md", catalog)
			if !strings.HasSuffix(f.read(".agents/skills/catalog/SKILL.md"), "A literal price is $2.50; the GraphQL type is `Float!`.\n") {
				t.Fatal("destination lost literal catalog body")
			}
			f.expect(".agents/skills/VENDORED.md", "Owned by the native installer.\n")
			f.expect(".agents/skills/catalog/references/details.txt", branch)
			f.missing(".claude/skills/native-demo/SKILL.md")
			f.missing(".agents/skills/dynamic-demo/SKILL.md")
			f.expect(".agents/skills/native-demo/SKILL.md", "Do not adopt this native variant.\n")
			before := auditTree(t, f.dir)
			f.apply()
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("second sync was not a no-op")
			}
			f.write("packages/component/AGENTS.md", branch+" reverse edit.\n")
			f.apply()
			f.expect("packages/component/CLAUDE.md", branch+" reverse edit.\n")
		})
	}
}
