package bridge

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func defaultHookFixture(t *testing.T, side, event string) *fixture {
	group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "/usr/bin/true"}}}
	if event == "SessionStart" {
		group["matcher"] = "^startup$"
	}
	if event == "PreToolUse" || event == "PostToolUse" {
		group["matcher"] = "^Bash$"
	}
	data, err := json.Marshal(map[string]any{"hooks": map[string]any{event: []any{group}}})
	must(t, err)
	f := portableFixture(t, "hook-config", string(data))
	if side == "codex" {
		must(t, os.Rename(f.path("claude-source"), f.path("codex-source")))
	}
	return f
}

func TestHookDefaultsUseSourceHostTimeout(t *testing.T) {
	for _, side := range []string{"claude", "codex"} {
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PreToolUse", "PostToolUse"} {
			t.Run(side+"/"+event, func(t *testing.T) {
				f := defaultHookFixture(t, side, event)
				original := f.read(side + "-source")
				f.apply()
				f.expect(side+"-source", original)
				target := "codex"
				if side == "codex" {
					target = "claude"
				}
				want := json.Number("600")
				if side == "claude" && event == "UserPromptSubmit" {
					want = "30"
				}
				raw, err := snapshot(f.path(target + "-source"))
				must(t, err)
				doc, err := document("claude", raw)
				must(t, err)
				group := doc["hooks"].(map[string]any)[event].([]any)[0].(map[string]any)
				if group["hooks"].([]any)[0].(map[string]any)["timeout"] != want {
					t.Fatal("source timeout not explicit in target")
				}
				for _, item := range f.plan().Items {
					if len(item.Writes) != 0 {
						t.Fatal("default timeout mapping did not converge")
					}
				}
			})
		}
	}
}

func TestHookDefaultsPreserveConflictAndHistory(t *testing.T) {
	f := defaultHookFixture(t, "claude", "UserPromptSubmit")
	tx := initialHistory(t, f)
	// Removing Codex's explicit 30 chooses its native default of 600; that
	// semantic change must not be dismissed as equivalent missing metadata.
	f.write("codex-source", `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}]}}`)
	f.write("claude-source", strings.Replace(f.read("claude-source"), "/usr/bin/true", "/bin/true", 1))
	if !f.plan().HasConflicts() {
		t.Fatal("different native defaults bypassed conflict detection")
	}
	choices := map[string]string{f.c.Resources[0].ID: "codex"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read("claude-source"), "600") {
		t.Fatal("reverse default change lost")
	}
	choice := HistoryChoice{tx, f.c.Resources[0].ID, "codex", "after"}
	history, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), history.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("codex-source"), "30") {
		t.Fatal("history lost original source default")
	}
}

func TestHookTimeoutExtendedRangeAndInvalidValues(t *testing.T) {
	for _, seconds := range []string{"61", "120", "600"} {
		f := portableFixture(t, "hook-config", strings.Replace(pluginHookFixture, "10", seconds, 1))
		f.apply()
		if !strings.Contains(f.read("codex-source"), seconds) {
			t.Fatal("explicit supported timeout lost")
		}
	}
	for _, seconds := range []string{"0", "-1", "601", "1.5", `"600"`, "null"} {
		f := portableFixture(t, "hook-config", pluginHookFixture)
		f.apply()
		f.write("claude-source", strings.Replace(pluginHookFixture, "10", seconds, 1))
		before := auditTree(t, f.dir)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("invalid timeout accepted")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("invalid timeout wrote files")
		}
	}
	f := defaultHookFixture(t, "claude", "Stop")
	raw, err := snapshot(f.path("claude-source"))
	must(t, err)
	doc, err := document("claude", raw)
	must(t, err)
	shared, err := encoded(doc["hooks"])
	must(t, err)
	if _, err := normalizeHooks("shared", shared); err == nil {
		t.Fatal("shared canonical data inferred a host default")
	}
}
