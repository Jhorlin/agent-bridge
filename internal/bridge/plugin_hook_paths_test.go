package bridge

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func pluginRelativeHookFixture(t *testing.T) *fixture {
	f := pluginFixture(t)
	f.write("claude-plugin/hooks/run.sh", "#!/bin/sh\nexit 0\n")
	must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0755))
	f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"`, 1))
	return f
}

func TestPluginRelativeHookRoundTripAndRecovery(t *testing.T) {
	f := pluginRelativeHookFixture(t)
	tx := initialHistory(t, f)
	f.expect("codex-plugin/hooks/run.sh", f.read("claude-plugin/hooks/run.sh"))
	if !strings.Contains(f.read("codex-plugin/hooks/hooks.json"), `${CLAUDE_PLUGIN_ROOT}/hooks/run.sh`) {
		t.Fatal("package-relative command missing from generated hook")
	}
	f.write("codex-plugin/hooks/run.sh", "#!/bin/sh\n# Reverse edit\nexit 0\n")
	must(t, os.Chmod(f.path("codex-plugin/hooks/run.sh"), 0755))
	before := f.read("claude-plugin/hooks/run.sh")
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("claude-plugin/hooks/run.sh", before)
	f.apply()
	f.expect("claude-plugin/hooks/run.sh", f.read("codex-plugin/hooks/run.sh"))
	choice := HistoryChoice{tx, f.c.Resources[0].ID + "/hooks/run.sh", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	f.expect("claude-plugin/hooks/run.sh", before)
	for _, item := range f.plan().Summaries() {
		if item.Status != "in-sync" {
			t.Fatal("relative hooks did not converge")
		}
	}
}

func TestPluginRelativeHookUnsafeInputsFailWithoutWrites(t *testing.T) {
	for _, command := range []string{
		`${CLAUDE_PLUGIN_ROOT}/hooks/run.sh`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/../run.sh"`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/missing.sh"`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/hooks.json"`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh" --flag`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh"; echo bad`,
		`"${OTHER_ROOT}/hooks/run.sh"`,
		`"${CLAUDE_PLUGIN_ROOT}/hooks/a$(id).sh"`,
	} {
		t.Run(command, func(t *testing.T) {
			f := pluginRelativeHookFixture(t)
			f.apply()
			f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", strings.ReplaceAll(command, `"`, `\"`), 1))
			before := auditTree(t, f.dir)
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("unsafe package command accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("rejection wrote files")
			}
		})
	}
	for _, kind := range []string{"not-executable", "symlink", "global"} {
		t.Run(kind, func(t *testing.T) {
			f := pluginRelativeHookFixture(t)
			switch kind {
			case "not-executable":
				must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0600))
			case "symlink":
				must(t, os.Remove(f.path("claude-plugin/hooks/run.sh")))
				must(t, os.Symlink("/usr/bin/true", f.path("claude-plugin/hooks/run.sh")))
			case "global":
				raw, err := snapshot(f.path("claude-plugin/hooks/hooks.json"))
				must(t, err)
				if _, err := normalizeHooks("claude", raw); err == nil {
					t.Fatal("global hook accepted plugin-relative root")
				}
				return
			}
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("unsafe dependency accepted")
			}
		})
	}
}

func TestPluginRelativeHookProspectiveDependencies(t *testing.T) {
	t.Run("conflict choice cannot break dependency", func(t *testing.T) {
		f := pluginRelativeHookFixture(t)
		f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
		f.apply()
		f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"`, 1))
		f.write("codex-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", "/bin/true", 1))
		must(t, os.Chmod(f.path("codex-plugin/hooks/run.sh"), 0600))
		choices := map[string]string{f.c.Resources[0].ID + "/hooks/hooks.json": "claude"}
		plan := f.plan()
		if !plan.HasConflicts() {
			t.Fatal("expected hook conflict")
		}
		if _, err := ReviewResolution(f.path("config.json"), choices); err == nil {
			t.Fatal("invalid dependency choice was reviewable")
		}
		before := auditTree(t, f.dir)
		_, err := Apply(f.c, Options{Resolutions: choices, ExpectedObservation: resolutionObservation(f.c, plan, choices)})
		if err == nil || !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("invalid dependency choice wrote files")
		}
		choices[f.c.Resources[0].ID+"/hooks/hooks.json"] = "codex"
		review, err := ReviewResolution(f.path("config.json"), choices)
		must(t, err)
		_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
		must(t, err)
	})
	t.Run("merged permission and reference edits", func(t *testing.T) {
		f := pluginRelativeHookFixture(t)
		f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
		f.apply()
		f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"`, 1))
		must(t, os.Chmod(f.path("codex-plugin/hooks/run.sh"), 0600))
		before := auditTree(t, f.dir)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("merged package lost executable hook dependency")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("invalid prospective dependency wrote files")
		}
	})
	t.Run("historical non-executable script", func(t *testing.T) {
		f := pluginRelativeHookFixture(t)
		f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
		must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0600))
		tx := initialHistory(t, f)
		must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0755))
		f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"`, 1))
		f.apply()
		choice := HistoryChoice{tx, f.c.Resources[0].ID + "/hooks/run.sh", "codex", "after"}
		if _, err := ReviewHistory(f.path("config.json"), choice); err == nil {
			t.Fatal("history selected non-executable hook dependency")
		}
	})
}

func TestPluginHookReservedPathCaseAlias(t *testing.T) {
	f := pluginRelativeHookFixture(t)
	must(t, os.Rename(f.path("claude-plugin/hooks/hooks.json"), f.path("claude-plugin/hooks/HOOKS.JSON")))
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("reserved hook configuration alias was treated as raw supporting content")
	}
}

func TestPluginRelativeHookSupportingFileChanges(t *testing.T) {
	setup := func(t *testing.T) *fixture {
		f := pluginFixture(t)
		f.write("claude-plugin/scripts/run.sh", "#!/bin/sh\nexit 0\n")
		must(t, os.Chmod(f.path("claude-plugin/scripts/run.sh"), 0755))
		f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
		f.apply()
		return f
	}
	for _, rename := range []string{"", "scripts/new.sh"} {
		t.Run("remove-or-move/"+rename, func(t *testing.T) {
			f := setup(t)
			f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/scripts/run.sh\"`, 1))
			f.apply()
			change := FileChange{Key: f.c.Resources[0].ID + "/scripts/run.sh", Rename: rename}
			before := auditTree(t, f.dir)
			if _, err := ReviewFileChange(f.path("config.json"), change); err == nil {
				t.Fatal("referenced executable removal was reviewable")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("review changed files")
			}
		})
	}
	t.Run("undo removes newly referenced destination", func(t *testing.T) {
		f := setup(t)
		change := FileChange{Key: f.c.Resources[0].ID + "/scripts/run.sh", Rename: "scripts/new.sh"}
		review, err := ReviewFileChange(f.path("config.json"), change)
		must(t, err)
		result, err := ApplyFileChange(f.path("config.json"), review.Observation, change)
		must(t, err)
		f.write("claude-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", `\"${CLAUDE_PLUGIN_ROOT}/scripts/new.sh\"`, 1))
		f.apply()
		if _, err := ReviewFileChangeUndo(f.path("config.json"), result.Transaction); err == nil {
			t.Fatal("undo could remove newly referenced executable")
		}
	})
}
