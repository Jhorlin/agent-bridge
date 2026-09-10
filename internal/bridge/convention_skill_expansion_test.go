package bridge

import (
	"reflect"
	"strings"
	"testing"
)

// Automatic discovery must not silently certify a host-specific skill as
// portable, even when reserved argument syntax is embedded in an identifier.
func TestConventionSkillDynamicExpansionBlocksAdoptionWithoutWrites(t *testing.T) {
	for _, source := range []string{"claude", "codex"} {
		for _, body := range []string{
			"Use $ARGUMENTS_SUFFIX.",
			"Use $1abc.",
			"Use $12suffix.",
			`Show \$ARGUMENTS_SUFFIX and \$1abc.`,
			"Use $CLAUDE_PLUGIN_ROOT.",
			"Use ${CLAUDE_PROJECT_DIR}.",
			"Use ${CLAUDE_FUTURE_TOKEN}.",
			"Use ${CLAUDE_UNTERMINATED.",
			"```!\nprintf inert-fixture\n```\n",
			"   ```!\nprintf inert-fixture\n   ```\n",
			"````!\nprintf inert-fixture\n````\n",
		} {
			t.Run(source+"/"+body, func(t *testing.T) {
				f := allConventionFixture(t, "project")
				reloadConventions(t, f)
				f.apply()
				from, to := ".claude/skills/demo/SKILL.md", ".agents/skills/demo/SKILL.md"
				if source == "codex" {
					from, to = to, from
				}
				f.write(from, "---\nname: demo\ndescription: Inert discovery fixture.\n---\n"+body+"\n")
				reloadConventions(t, f)
				before := auditTree(t, f.dir)
				wrote := false
				_, err := Apply(f.c, Options{BeforeWrite: func(int, Operation) error { wrote = true; return nil }})
				contains(t, err, "explicit compatibility review required")
				if wrote || !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("blocked automatic skill adoption changed fixture files")
				}
				f.missing(to)
				f.missing("state/pending.json")
			})
		}
	}
}

func TestConventionSkillLiteralDollarExamplesRemainPortable(t *testing.T) {
	const body = "Match regex `\\.env$|node_modules/` or `^literal$`.\n" +
		"Show $ USD, $branch, ${HOME}, and https://example.invalid/$(printf inert)/file.\n" +
		"```sh\nprintf 'literal example'\n```\n"
	for _, source := range []string{"claude", "codex"} {
		t.Run(source, func(t *testing.T) {
			f := allConventionFixture(t, "project")
			from, to := ".claude/skills/demo/SKILL.md", ".agents/skills/demo/SKILL.md"
			if source == "codex" {
				from, to = to, from
			}
			f.write(from, "---\nname: demo\ndescription: Literal example fixture.\n---\n"+body)
			reloadConventions(t, f)
			f.apply()
			if !strings.HasSuffix(f.read(to), body) {
				t.Fatal("literal dollar examples changed during automatic adoption")
			}
			f.apply()
		})
	}
}

func TestExplicitReviewedSkillRetainsDynamicBody(t *testing.T) {
	const body = "Use $ARGUMENTS_SUFFIX, $1abc, and ${CLAUDE_SKILL_DIR}.\n```!\nprintf inert-fixture\n```\n"
	for _, source := range []string{"claude", "codex"} {
		t.Run(source, func(t *testing.T) {
			f := allConventionFixture(t, "project")
			// Explicit portable:true remains the user's compatibility decision,
			// even with conventions active for other discovered resources.
			f.raw.Conventions = &Conventions{Root: ".", Features: []string{"skills"}}
			f.raw.Resources = []resourceInput{{ID: "reviewed-demo", Kind: "skill-directory", Scope: "project", Portable: true, Claude: ".claude/skills/demo", Codex: ".agents/skills/demo"}}
			from, to := ".claude/skills/demo/SKILL.md", ".agents/skills/demo/SKILL.md"
			if source == "codex" {
				from, to = to, from
			}
			f.write(from, "---\nname: demo\ndescription: Reviewed fixture.\n---\n"+body)
			f.load()
			f.apply()
			f.expect(to, f.read(from))
		})
	}
}
