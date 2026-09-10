package bridge

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func nativePluginGuardFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["plugins"],"protectNativePlugins":true},"resources":[]}`)
	f.write(".agent-bridge-plugins/claude/alias/.claude-plugin/plugin.json", `{"name":"demo","version":"1.0.0"}`)
	reloadConventions(t, f)
	return f
}

func TestNativePluginGuardBlocksCachedNameWithoutWrites(t *testing.T) {
	for _, host := range []string{".claude", ".codex"} {
		for _, manifest := range []string{".claude-plugin/plugin.json", ".codex-plugin/plugin.json", "plugin.json"} {
			t.Run(host+"/"+manifest, func(t *testing.T) {
				f := nativePluginGuardFixture(t)
				f.apply()
				f.write(host+"/plugins/cache/private-vendor/different-directory/1/"+manifest, `{"name":"demo","version":"99.0.0","author":"private-author"}`)
				before := auditTree(t, f.dir)
				_, err := LoadConfig(f.path("config.json"))
				contains(t, err, "cached candidate")
				if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), f.dir) {
					t.Fatal("error exposed private metadata")
				}
				if _, err = Apply(f.c, Options{}); err == nil {
					t.Fatal("stale loaded config bypassed native guard")
				}
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("native guard wrote files")
				}
			})
		}
	}
}

func TestNativePluginGuardReadsOnlyIdentityAndHonorsOptIn(t *testing.T) {
	f := nativePluginGuardFixture(t)
	cache := ".codex/plugins/cache/vendor/pkg/1/"
	f.write(cache+".codex-plugin/plugin.json", `{"name":"unrelated"}`)
	must(t, os.Symlink("missing-body", f.path(cache+"body")))
	reloadConventions(t, f)
	f.apply()
	f.write(cache+".codex-plugin/plugin.json", `{"name":"demo"}`)
	f.write("config.json", strings.Replace(f.read("config.json"), `"protectNativePlugins":true`, `"protectNativePlugins":false`, 1))
	reloadConventions(t, f)
	f.apply()
	// A reviewed explicit exclusion leaves native ownership alone.
	f.write("config.json", `{"version":1,"stateDir":"state2","conventions":{"root":".","scope":"global","features":["plugins"],"protectNativePlugins":true,"exclude":[".agent-bridge-plugins"]},"resources":[]}`)
	reloadConventions(t, f)
	if len(f.c.Resources) != 0 {
		t.Fatal("exclusion did not protect native ownership")
	}
}

func TestNativePluginGuardRejectsIncompleteInventory(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "duplicate-key", "invalid-utf8", "unknown-schema", "oversized", "manifest-link", "cache-link"} {
		t.Run(kind, func(t *testing.T) {
			f := nativePluginGuardFixture(t)
			path := ".codex/plugins/cache/vendor/pkg/1/"
			f.write(path+".codex-plugin/plugin.json", `{"name":"unrelated"}`)
			switch kind {
			case "missing":
				must(t, os.Remove(f.path(path+".codex-plugin/plugin.json")))
			case "malformed":
				f.write(path+".codex-plugin/plugin.json", `{"name":`)
			case "duplicate-key":
				f.write(path+".codex-plugin/plugin.json", `{"name":"demo","name":"unrelated"}`)
			case "invalid-utf8":
				f.write(path+".codex-plugin/plugin.json", "{\"name\":\"bad\xff\"}")
			case "unknown-schema":
				f.write(path+".codex-plugin/plugin.json", `{"name":"unrelated","$schema":"https://unknown.invalid/schema"}`)
			case "oversized":
				must(t, os.Truncate(f.path(path+".codex-plugin/plugin.json"), candidateManifestLimit+1))
			case "manifest-link":
				must(t, os.Rename(f.path(path+".codex-plugin/plugin.json"), f.path(path+"target")))
				must(t, os.Symlink("../target", f.path(path+".codex-plugin/plugin.json")))
			case "cache-link":
				must(t, os.Rename(f.path(".codex/plugins/cache"), f.path("cache-target")))
				must(t, os.Symlink(f.path("cache-target"), f.path(".codex/plugins/cache")))
			}
			before := auditTree(t, f.dir)
			_, err := LoadConfig(f.path("config.json"))
			contains(t, err, "inventory unavailable")
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("blocked inventory wrote files")
			}
		})
	}
}

func TestNativePluginGuardRejectsProspectiveCanonicalName(t *testing.T) {
	f := nativePluginGuardFixture(t)
	f.apply()
	f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"future"}`)
	id := f.c.Resources[0].ID
	f.write("state/shared/"+id+"/plugin.json", `{"name":"future","version":"1.0.0"}`)
	reloadConventions(t, f)
	before := auditTree(t, f.dir)
	_, err := Apply(f.c, Options{})
	contains(t, err, "cached candidate")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("prospective collision wrote files")
	}
}

func TestNativePluginGuardRequiresGlobalPlugins(t *testing.T) {
	for _, policy := range []string{
		`"scope":"project","features":["plugins"]`,
		`"features":["plugins"]`,
		`"scope":"global","features":["skills"]`,
	} {
		f := newFixture(t)
		f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","protectNativePlugins":true,`+policy+`},"resources":[]}`)
		_, err := LoadConfig(f.path("config.json"))
		contains(t, err, "requires global plugin conventions")
	}
}

func TestNativePluginGuardRechecksImmediatelyBeforeJournal(t *testing.T) {
	f := nativePluginGuardFixture(t)
	called := false
	_, err := Apply(f.c, Options{beforePrepare: func() {
		called = true
		f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"demo"}`)
	}})
	contains(t, err, "cached candidate")
	if !called {
		t.Fatal("fixture did not reach preparation")
	}
	f.missing("state/manifest.json")
	f.missing("state/pending.json")
	f.missing(".agent-bridge-plugins/codex/alias/.codex-plugin/plugin.json")
}

func nativePluginRenameFixture(t *testing.T) (*fixture, string, string) {
	f := nativePluginGuardFixture(t)
	id := f.c.Resources[0].ID
	tx := initialHistory(t, f)
	f.write(".agent-bridge-plugins/claude/alias/.claude-plugin/plugin.json", `{"name":"renamed","version":"1.0.0"}`)
	reloadConventions(t, f)
	f.apply()
	return f, id, tx
}

func TestNativePluginGuardHistoricalNameAndFreshCache(t *testing.T) {
	for _, collide := range []bool{false, true} {
		f, id, tx := nativePluginRenameFixture(t)
		choice := HistoryChoice{tx, id + "/@manifest", "codex", "after"}
		review, err := ReviewHistory(f.path("config.json"), choice)
		must(t, err)
		if collide {
			f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"demo"}`)
		}
		before := auditTree(t, f.dir)
		_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
		if !collide {
			must(t, err)
			if !strings.Contains(f.read(".agent-bridge-plugins/claude/alias/.claude-plugin/plugin.json"), `"demo"`) {
				t.Fatal("noncolliding history not restored")
			}
			continue
		}
		if err == nil {
			t.Fatal("restored competing historical name")
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("blocked history wrote files")
		}
		if _, err := ReviewHistory(f.path("config.json"), choice); err == nil {
			t.Fatal("review accepted collision")
		}
	}
}

func TestNativePluginGuardConflictSelectedName(t *testing.T) {
	f, id, _ := nativePluginRenameFixture(t)
	f.write("state/shared/"+id+"/plugin.json", `{"name":"demo","version":"1.0.0"}`)
	f.write(".agent-bridge-plugins/claude/alias/.claude-plugin/plugin.json", `{"name":"renamed","version":"2.0.0"}`)
	choices := map[string]string{id + "/@manifest": "shared"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"demo"}`)
	before := auditTree(t, f.dir)
	if _, err := ResolveReviewed(f.path("config.json"), review.Observation, choices); err == nil {
		t.Fatal("resolved competing name")
	}
	if _, err := ReviewResolution(f.path("config.json"), choices); err == nil {
		t.Fatal("review accepted colliding choice")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("blocked resolution wrote files")
	}
}

func TestNativePluginGuardTwoProspectiveNames(t *testing.T) {
	f := nativePluginGuardFixture(t)
	f.write(".agent-bridge-plugins/claude/second/.claude-plugin/plugin.json", `{"name":"second"}`)
	reloadConventions(t, f)
	f.apply()
	for _, r := range f.c.Resources {
		f.write("state/shared/"+r.ID+"/plugin.json", `{"name":"future"}`)
	}
	before := auditTree(t, f.dir)
	_, err := Apply(f.c, Options{})
	contains(t, err, "competing names")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("two prospective names wrote files")
	}
}

func TestNativePluginGuardPolicyRoundTrip(t *testing.T) {
	f := nativePluginGuardFixture(t)
	data, err := json.Marshal(f.c.Conventions)
	must(t, err)
	var policy Conventions
	must(t, json.Unmarshal(data, &policy))
	if !policy.ProtectNativePlugins || !reflect.DeepEqual(*f.c.Conventions, policy) {
		t.Fatal("native plugin policy lost in serialization")
	}
}

func TestNativePluginGuardRecoveryPausesUntilCollisionResolved(t *testing.T) {
	f := nativePluginGuardFixture(t)
	f.apply()
	f.write(".agent-bridge-plugins/claude/alias/.claude-plugin/plugin.json", `{"name":"demo","version":"2.0.0"}`)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("crash not injected")
			}
		}()
		_, _ = Apply(f.c, Options{BeforeWrite: func(i int, _ Operation) error {
			if i == 1 {
				panic("fixture crash")
			}
			return nil
		}})
	}()
	if f.read("state/pending.json") == "" {
		t.Fatal("fixture has no pending journal")
	}
	cache := ".codex/plugins/cache/vendor/pkg/1/plugin.json"
	f.write(cache, `{"name":"demo"}`)
	before := auditTree(t, f.dir)
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "cached candidate")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("blocked reload changed pending recovery")
	}
	// Simulate independently resolving ownership, not bypassing protection.
	f.write(cache, `{"name":"different-native-plugin"}`)
	reloadConventions(t, f)
	_, err = Recover(f.c)
	must(t, err)
	f.missing("state/pending.json")
	f.apply()
}

func TestNativePluginGuardDualManifestsAndProspectiveDuplicate(t *testing.T) {
	f := nativePluginGuardFixture(t)
	f.write(".codex/plugins/cache/vendor/pkg/1/.codex-plugin/plugin.json", `{"name":"unrelated"}`)
	f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"demo"}`)
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "cached candidate")
	f.write(".codex/plugins/cache/vendor/pkg/1/plugin.json", `{"name":"other"}`)
	f.write(".agent-bridge-plugins/claude/second/.claude-plugin/plugin.json", `{"name":"demo"}`)
	_, err = LoadConfig(f.path("config.json"))
	contains(t, err, "competing names")
}
