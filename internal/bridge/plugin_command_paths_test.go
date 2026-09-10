package bridge

import (
	"reflect"
	"strings"
	"testing"
)

func TestPluginCommandNestedAndUnderscorePaths(t *testing.T) {
	for _, name := range []string{"group/nested", "clean_gone"} {
		f := pluginFixture(t)
		f.write("claude-plugin/commands/"+name+".md", commandFixture)
		tx := initialHistory(t, f)
		f.write("codex-plugin/commands/"+name+".md", strings.Replace(commandFixture, "fixed word", "reverse word", 1))
		f.apply()
		if !strings.Contains(f.read("claude-plugin/commands/"+name+".md"), "reverse word") {
			t.Fatal("reverse command edit missing")
		}
		choice := HistoryChoice{tx, f.c.Resources[0].ID + "/commands/" + name + ".md", "codex", "after"}
		review, err := ReviewHistory(f.path("config.json"), choice)
		must(t, err)
		_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
		must(t, err)
		f.expect("claude-plugin/commands/"+name+".md", commandFixture)
		for _, item := range f.plan().Items {
			if len(item.Writes) != 0 {
				t.Fatal("command did not converge")
			}
		}
	}
}

func TestPluginCommandNormalizedNameCollisions(t *testing.T) {
	for _, pair := range [][2]string{{"group/nested", "group-nested"}, {"clean_gone", "clean-gone"}, {"group/nested_name", "group-nested-name"}} {
		f := pluginFixture(t)
		f.write("claude-plugin/commands/"+pair[0]+".md", commandFixture)
		f.apply()
		f.write("codex-plugin/commands/"+pair[1]+".md", commandFixture)
		before := auditTree(t, f.dir)
		_, err := Apply(f.c, Options{})
		contains(t, err, "collid")
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("colliding command wrote files")
		}
	}
	f := pluginFixture(t)
	f.write("claude-plugin/commands/group/nested_name.md", commandFixture)
	f.write("claude-plugin/skills/source-command-group-nested-name/SKILL.md", "---\nname: source-command-group-nested-name\ndescription: Fixture.\n---\nFixture body.\n")
	_, err := Apply(f.c, Options{})
	contains(t, err, "collid")
}

func TestPluginCommandLiteralDollars(t *testing.T) {
	for _, body := range []string{`Match \.env$|config$`, `$branch $HOME ${HOME} ${UNKNOWN} ${ARGUMENTS}`, `$ USD $(printf inert)`} {
		f := pluginFixture(t)
		f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "Return the fixed word fixture.", body, 1))
		f.apply()
		if !strings.Contains(f.read("codex-plugin/commands/demo.md"), body) {
			t.Fatal("literal dollar text was altered")
		}
		f.write("codex-plugin/commands/demo.md", f.read("codex-plugin/commands/demo.md")+"Reverse text.\n")
		f.apply()
		if !strings.Contains(f.read("claude-plugin/commands/demo.md"), body) {
			t.Fatal("reverse dollar text was altered")
		}
	}
}

func TestPluginCommandRejectsDynamicReservedTokens(t *testing.T) {
	for _, token := range []string{"$ARGUMENTS", "$ARGUMENTS_suffix", "$0", "$1abc", `\$1`, `\$ARGUMENTS`, "${CLAUDE_PLUGIN_ROOT}", "$CLAUDE_SESSION_ID", "${CLAUDE_SESSION_ID}", "!`echo fixture`"} {
		f := pluginFixture(t)
		f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "fixed word", token, 1))
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatalf("dynamic token %q accepted", token)
		}
		f.missing("codex-plugin")
	}
}

func TestPluginCommandRejectsFencedShellPreprocessing(t *testing.T) {
	f := pluginFixture(t)
	f.write("claude-plugin/commands/demo.md", commandFixture+"\n```!\nprintf fixture\n```\n")
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("fenced shell preprocessing accepted as static text")
	}
	f.missing("codex-plugin")
}
