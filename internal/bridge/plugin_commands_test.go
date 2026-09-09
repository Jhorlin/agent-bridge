package bridge

import (
	"os"
	"strings"
	"testing"
)

const commandFixture = "---\ndescription: A portable command.\n---\nReturn the fixed word fixture.\n"

func TestPluginCommandTranslation(t *testing.T) {
	f := pluginFixture(t)
	f.write("claude-plugin/commands/demo.md", commandFixture)
	f.apply()
	first := f.read("codex-plugin/commands/demo.md")
	f.write("codex-plugin/commands/demo.md", strings.Replace(first, "fixed word", "updated word", 1))
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("claude-plugin/commands/demo.md", commandFixture)
	f.apply()
	if !strings.Contains(f.read("claude-plugin/commands/demo.md"), "updated word") {
		t.Fatal("reverse command edit missing")
	}
	baseline := f.read("state/manifest.json")
	f.apply()
	f.expect("state/manifest.json", baseline)
	f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "fixed word", "Claude choice", 1))
	f.write("codex-plugin/commands/demo.md", strings.Replace(commandFixture, "fixed word", "Codex choice", 1))
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
	choices := map[string]string{f.c.Resources[0].ID + "/commands/demo.md": "codex"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read("claude-plugin/commands/demo.md"), "Codex choice") {
		t.Fatal("reviewed command conflict not resolved")
	}
	must(t, os.Remove(f.path("codex-plugin/commands/demo.md")))
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
}

func TestPluginCommandsFailClosed(t *testing.T) {
	for _, raw := range []string{
		"no frontmatter", strings.Replace(commandFixture, "description:", "model:", 1),
		strings.Replace(commandFixture, "---\nReturn", "allowed-tools: Bash\n---\nReturn", 1),
		strings.Replace(commandFixture, "fixed word", "$ARGUMENTS", 1),
		strings.Replace(commandFixture, "fixed word", "!`id`", 1),
		"---\ndescription: One\ndescription: Two\n---\nBody\n",
	} {
		f := pluginFixture(t)
		f.write("claude-plugin/commands/demo.md", raw)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsafe command accepted")
		}
		f.missing("codex-plugin")
	}
	for _, path := range []string{"commands/nested/demo.md", "commands/UPPER.md", "commands/demo.sh"} {
		f := pluginFixture(t)
		f.write("claude-plugin/"+path, commandFixture)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsupported command path accepted")
		}
	}
	f := pluginFixture(t)
	f.write("claude-plugin/commands/demo.md", commandFixture)
	f.write("claude-plugin/skills/source-command-demo/SKILL.md", "---\nname: source-command-demo\ndescription: Collision.\n---\nBody\n")
	_, err := Apply(f.c, Options{})
	contains(t, err, "collides")
}

func TestPluginCommandHistoryAndStaleReview(t *testing.T) {
	f := pluginFixture(t)
	f.write("claude-plugin/commands/demo.md", commandFixture)
	tx := initialHistory(t, f)
	f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "fixed word", "updated word", 1))
	f.apply()
	choice := HistoryChoice{tx, f.c.Resources[0].ID + "/commands/demo.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	f.write("codex-plugin/commands/demo.md", f.read("codex-plugin/commands/demo.md")+"\n")
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	contains(t, err, "changed")
	review, err = ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("codex-plugin/commands/demo.md"), "fixed word") {
		t.Fatal("historical command body not restored")
	}
}
