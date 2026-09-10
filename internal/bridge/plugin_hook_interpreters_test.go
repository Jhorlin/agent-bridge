package bridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func interpretedHookFixture(t *testing.T, interpreter string) *fixture {
	f := pluginFixture(t)
	f.write("claude-plugin/hooks-handlers/run.sh", "printf '%s' fixture\n")
	f.write("claude-plugin/scripts/input.txt", "inert argument fixture\n")
	command := interpreter + ` "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" "${CLAUDE_PLUGIN_ROOT}/scripts/input.txt"`
	data, err := json.Marshal(command)
	must(t, err)
	f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, `"/usr/bin/true"`, string(data), 1))
	return f
}

// Removing interpreter support must reject a valid non-executable script;
// dropping argument dependencies must allow an invalid supporting-file delete.
func TestPluginInterpretedHooksRoundTrip(t *testing.T) {
	for _, interpreter := range []string{"sh", "bash"} {
		t.Run(interpreter, func(t *testing.T) {
			f := interpretedHookFixture(t, interpreter)
			tx := initialHistory(t, f)
			f.expect("codex-plugin/scripts/input.txt", "inert argument fixture\n")
			original := f.read("claude-plugin/hooks-handlers/run.sh")
			f.write("codex-plugin/hooks-handlers/run.sh", "printf '%s' reverse\n")
			_, err := Apply(f.c, Options{BeforeWrite: failSecond})
			contains(t, err, "rolled back")
			f.expect("claude-plugin/hooks-handlers/run.sh", original)
			f.apply()
			f.expect("claude-plugin/hooks-handlers/run.sh", "printf '%s' reverse\n")
			choice := HistoryChoice{tx, f.c.Resources[0].ID + "/hooks-handlers/run.sh", "codex", "after"}
			review, err := ReviewHistory(f.path("config.json"), choice)
			must(t, err)
			_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
			must(t, err)
			f.expect("claude-plugin/hooks-handlers/run.sh", original)
			for _, item := range f.plan().Summaries() {
				if item.Status != "in-sync" {
					t.Fatal("interpreted hook did not converge")
				}
			}
		})
	}
}

func TestPluginInterpretedHooksRejectUnsafeCommands(t *testing.T) {
	for _, command := range []string{
		`bash -c "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh"`,
		`env bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh"`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" ; id`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" "${CLAUDE_PLUGIN_ROOT}/scripts/missing.txt"`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" "${CLAUDE_PLUGIN_ROOT}/scripts/../input.txt"`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks/hooks.json"`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" $(id)`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" "${OTHER}/scripts/input.txt"`,
		`bash  "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh"`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh" "${CLAUDE_PLUGIN_ROOT}/scripts/input.txt"`,
		`bash "${CLAUDE_PLUGIN_ROOT}/hooks-handlers/run.sh"` + strings.Repeat(` "${CLAUDE_PLUGIN_ROOT}/scripts/input.txt"`, 16),
	} {
		t.Run(command, func(t *testing.T) {
			f := interpretedHookFixture(t, "bash")
			f.apply()
			data, err := json.Marshal(command)
			must(t, err)
			f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, `"/usr/bin/true"`, string(data), 1))
			before := auditTree(t, f.dir)
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("unsafe command accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("rejected command wrote files")
			}
		})
	}
}

func TestPluginInterpretedHooksProtectArgumentDependencies(t *testing.T) {
	for _, rename := range []string{"", "scripts/renamed.txt"} {
		f := interpretedHookFixture(t, "bash")
		f.apply()
		before := auditTree(t, f.dir)
		_, err := ReviewFileChange(f.path("config.json"), FileChange{Key: f.c.Resources[0].ID + "/scripts/input.txt", Rename: rename})
		if err == nil {
			t.Fatal("referenced interpreter argument could be removed")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("rejected dependency change wrote files")
		}
	}
	f := interpretedHookFixture(t, "bash")
	f.apply()
	// Prospective validation must include the second file, not only the script.
	p := f.plan()
	for index := range p.Items {
		if p.Items[index].Relative == "scripts/input.txt" {
			p.Items[index].Content = nil
		}
	}
	if err := validatePlannedPluginContents(p); err == nil {
		t.Fatal("prospective hook lost its argument dependency")
	}
}

func TestPluginInterpretedHookHistoryCannotRestoreDeletedArgument(t *testing.T) {
	f := interpretedHookFixture(t, "bash")
	tx := initialHistory(t, f)
	f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	f.apply()
	change := FileChange{Key: f.c.Resources[0].ID + "/scripts/input.txt"}
	review, err := ReviewFileChange(f.path("config.json"), change)
	must(t, err)
	_, err = ApplyFileChange(f.path("config.json"), review.Observation, change)
	must(t, err)
	before := auditTree(t, f.dir)
	choice := HistoryChoice{tx, f.c.Resources[0].ID + "/hooks/hooks.json", "codex", "after"}
	if _, err := ReviewHistory(f.path("config.json"), choice); err == nil {
		t.Fatal("historical hook can restore a missing argument dependency")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("rejected history review wrote files")
	}
}

func TestPluginInterpretedHookConflictSelection(t *testing.T) {
	f := interpretedHookFixture(t, "bash")
	f.write("claude-plugin/scripts/second.txt", "second inert argument\n")
	f.apply()
	original := f.read("claude-plugin/hooks/hooks.json")
	f.write("claude-plugin/hooks/hooks.json", strings.Replace(original, "bash", "sh", 1))
	f.write("codex-plugin/hooks/hooks.json", strings.Replace(original, "input.txt", "second.txt", 1))
	if !f.plan().HasConflicts() {
		t.Fatal("distinct command changes did not conflict")
	}
	choices := map[string]string{f.c.Resources[0].ID + "/hooks/hooks.json": "codex"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read("claude-plugin/hooks/hooks.json"), "second.txt") {
		t.Fatal("selected interpreter argument lost")
	}
	if _, err := ReviewFileChange(f.path("config.json"), FileChange{Key: f.c.Resources[0].ID + "/scripts/second.txt"}); err == nil {
		t.Fatal("selected argument can be deleted")
	}
}
