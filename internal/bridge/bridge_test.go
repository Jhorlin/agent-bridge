package bridge

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fixture struct {
	t   *testing.T
	dir string
	c   Config
	raw configInput
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func contains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
		t.Fatalf("wanted %q error, got %v", want, err)
	}
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	f := &fixture{t: t, dir: dir, raw: configInput{Version: 1, StateDir: "state", Resources: []resourceInput{{ID: "rules", Kind: "portable-file", Scope: "global", Claude: "CLAUDE.md", Codex: "AGENTS.md"}}}}
	f.write("CLAUDE.md", "one")
	f.load()
	return f
}
func (f *fixture) path(name string) string { return filepath.Join(f.dir, name) }
func (f *fixture) write(name, data string) {
	f.t.Helper()
	must(f.t, os.MkdirAll(filepath.Dir(f.path(name)), 0700))
	must(f.t, os.WriteFile(f.path(name), []byte(data), 0644))
}
func (f *fixture) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(f.path(name))
	must(f.t, err)
	return string(b)
}
func (f *fixture) save() {
	b, err := json.Marshal(f.raw)
	must(f.t, err)
	f.write("config.json", string(b))
}
func (f *fixture) load() {
	f.save()
	c, err := LoadConfig(f.path("config.json"))
	must(f.t, err)
	f.c = c
}
func (f *fixture) apply() { f.t.Helper(); _, err := Apply(f.c, Options{}); must(f.t, err) }
func (f *fixture) expect(name, want string) {
	f.t.Helper()
	if got := f.read(name); got != want {
		f.t.Fatalf("%s: got %q, want %q", name, got, want)
	}
}
func (f *fixture) missing(name string) {
	f.t.Helper()
	_, err := os.Lstat(f.path(name))
	if !os.IsNotExist(err) {
		f.t.Fatalf("expected %s absent, got %v", name, err)
	}
}
func (f *fixture) plan() PlanResult { f.t.Helper(); p, err := Plan(f.c); must(f.t, err); return p }
func skillFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.write("claude-skill/SKILL.md", "---\nname: demo\ndescription: A portable demo.\n---\nUse scripts/run.sh.\n")
	f.write("claude-skill/scripts/run.sh", "#!/bin/sh\nprintf demo\n")
	must(t, os.Chmod(f.path("claude-skill/scripts/run.sh"), 0755))
	f.raw.Resources = []resourceInput{{ID: "demo", Kind: "skill-directory", Portable: true, Scope: "global", Claude: "claude-skill", Codex: "codex-skill"}}
	f.load()
	return f
}

func TestPlanReadOnly(t *testing.T) {
	f := newFixture(t)
	if f.plan().Summaries()[0].Status != "pending" {
		t.Fatal("expected pending")
	}
	f.missing("state")
}
func TestBootstrapIdempotent(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.expect("AGENTS.md", "one")
	if f.plan().Summaries()[0].Status != "in-sync" {
		t.Fatal("drift")
	}
}
func TestEditsFromAllPeers(t *testing.T) {
	f := newFixture(t)
	f.apply()
	for _, file := range []string{"AGENTS.md", "CLAUDE.md", "state/shared/rules"} {
		f.write(file, file)
		f.apply()
		f.expect("AGENTS.md", file)
		f.expect("CLAUDE.md", file)
	}
}
func TestInitialConflict(t *testing.T) {
	f := newFixture(t)
	f.write("AGENTS.md", "different")
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.expect("CLAUDE.md", "one")
}
func TestConcurrentConflict(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "left")
	f.write("AGENTS.md", "right")
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.expect("CLAUDE.md", "left")
	f.expect("AGENTS.md", "right")
}
func TestIdenticalConcurrentEdits(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "same")
	f.write("AGENTS.md", "same")
	f.apply()
	f.expect("state/shared/rules", "same")
}
func TestDeletionConflict(t *testing.T) {
	f := newFixture(t)
	f.apply()
	must(t, os.Remove(f.path("CLAUDE.md")))
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.expect("AGENTS.md", "one")
}
func TestSymlinkRejected(t *testing.T) {
	f := newFixture(t)
	must(t, os.Symlink(f.path("CLAUDE.md"), f.path("AGENTS.md")))
	_, err := Plan(f.c)
	contains(t, err, "symlink")
}
func TestPrivateBackupJournal(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "two")
	f.apply()
	entries, err := os.ReadDir(f.path("state/backups"))
	must(t, err)
	found := false
	for _, e := range entries {
		file := "state/backups/" + e.Name() + "/journal.json"
		found = found || strings.Contains(f.read(file), "rules-codex")
		info, err := os.Stat(f.path(file))
		must(t, err)
		if info.Mode().Perm() != 0600 {
			t.Fatal("journal is not private")
		}
	}
	if !found {
		t.Fatal("missing backup")
	}
}
func TestLockExclusion(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("state/sync.lock", "")
	_, err := Apply(f.c, Options{})
	contains(t, err, "another sync")
}
func TestUnsupportedAdapter(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources[0].Kind = "plugin"
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "unsupported adapter")
}
func TestSkillBytesAndExecutables(t *testing.T) {
	f := skillFixture(t)
	f.write("claude-skill/binary", string([]byte{0, 255, 3}))
	f.apply()
	f.expect("codex-skill/SKILL.md", f.read("claude-skill/SKILL.md"))
	f.expect("codex-skill/binary", string([]byte{0, 255, 3}))
	info, err := os.Stat(f.path("codex-skill/scripts/run.sh"))
	must(t, err)
	if info.Mode().Perm()&0111 != 0111 {
		t.Fatal("executable bits lost")
	}
	for _, s := range f.plan().Summaries() {
		if s.Status != "in-sync" {
			t.Fatal(s)
		}
	}
}
func TestSkillIndependentEditsAndAdditions(t *testing.T) {
	f := skillFixture(t)
	f.apply()
	f.write("claude-skill/SKILL.md", "updated")
	f.write("codex-skill/scripts/run.sh", "#!/bin/sh\nprintf updated\n")
	f.write("codex-skill/new.txt", "new")
	f.apply()
	f.expect("codex-skill/SKILL.md", "updated")
	f.expect("claude-skill/new.txt", "new")
	f.expect("claude-skill/scripts/run.sh", "#!/bin/sh\nprintf updated\n")
}
func TestSkillConflictBlocksAll(t *testing.T) {
	f := skillFixture(t)
	f.apply()
	f.write("claude-skill/SKILL.md", "left")
	f.write("codex-skill/SKILL.md", "right")
	f.write("codex-skill/new.txt", "unrelated")
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.missing("claude-skill/new.txt")
}
func TestSkillDeletion(t *testing.T) {
	f := skillFixture(t)
	f.apply()
	must(t, os.Remove(f.path("codex-skill/scripts/run.sh")))
	_, err := Apply(f.c, Options{})
	contains(t, err, "conflicts")
	f.expect("claude-skill/scripts/run.sh", "#!/bin/sh\nprintf demo\n")
}
func TestSkillInvalidSources(t *testing.T) {
	f := skillFixture(t)
	must(t, os.Symlink(f.path("CLAUDE.md"), f.path("claude-skill/link")))
	_, err := Plan(f.c)
	contains(t, err, "symlink")
	must(t, os.Remove(f.path("claude-skill/link")))
	must(t, os.Remove(f.path("claude-skill/SKILL.md")))
	_, err = Plan(f.c)
	contains(t, err, "SKILL.md")
}
func TestSkillPortabilityRequired(t *testing.T) {
	f := skillFixture(t)
	f.raw.Resources[0].Portable = false
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "portable: true")
}
func TestModeOnlyChanges(t *testing.T) {
	f := newFixture(t)
	f.apply()
	must(t, os.Chmod(f.path("CLAUDE.md"), 0744))
	f.apply()
	info, err := os.Stat(f.path("AGENTS.md"))
	must(t, err)
	if info.Mode().Perm() != 0700 {
		t.Fatal("rw access widened or execute bit lost")
	}
}
func failSecond(index int, _ Operation) error {
	if index == 1 {
		return errors.New("injected failure")
	}
	return nil
}
func TestPartialFailureRollback(t *testing.T) {
	f := newFixture(t)
	f.apply()
	manifest := f.read("state/manifest.json")
	f.write("CLAUDE.md", "new edit")
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("AGENTS.md", "one")
	f.expect("state/shared/rules", "one")
	f.expect("state/manifest.json", manifest)
	f.expect("CLAUDE.md", "new edit")
	r, err := Recover(f.c)
	must(t, err)
	if r.Status != "nothing-to-recover" {
		t.Fatal(r)
	}
	f.apply()
	f.expect("AGENTS.md", "new edit")
}
func TestBootstrapFailureCleanup(t *testing.T) {
	f := newFixture(t)
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("CLAUDE.md", "one")
	f.missing("state/shared/rules")
	f.missing("AGENTS.md")
}
func TestLaterEditBlocksRecovery(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "new")
	_, err := Apply(f.c, Options{BeforeWrite: func(i int, _ Operation) error {
		if i == 1 {
			f.write("state/shared/rules", "later edit")
			return errors.New("fail")
		}
		return nil
	}})
	contains(t, err, "pending transaction retained")
	_, err = Recover(f.c)
	contains(t, err, "later edit")
	_, err = Plan(f.c)
	contains(t, err, "requires recover")
	f.expect("state/shared/rules", "later edit")
	f.write("state/shared/rules", "new")
	r, err := Recover(f.c)
	must(t, err)
	if r.Status != "recovered" {
		t.Fatal(r)
	}
	f.expect("state/shared/rules", "one")
}
func TestCrashHelper(t *testing.T) {
	filename := os.Getenv("AGENT_BRIDGE_CRASH_CONFIG")
	if filename == "" {
		t.Skip("subprocess helper")
	}
	c, err := LoadConfig(filename)
	must(t, err)
	_, err = Apply(c, Options{BeforeWrite: func(i int, _ Operation) error {
		if i == 1 {
			os.Exit(91)
		}
		return nil
	}})
	t.Fatalf("helper did not crash: %v", err)
}
func TestProcessInterruptionRecovery(t *testing.T) {
	f := newFixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), "AGENT_BRIDGE_CRASH_CONFIG="+f.path("config.json"))
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 91 {
		t.Fatalf("unexpected helper exit: %v", err)
	}
	_, err = Recover(f.c)
	contains(t, err, "stale sync.lock")
	must(t, os.Remove(f.path("state/sync.lock")))
	r, err := Recover(f.c)
	must(t, err)
	if r.Status != "recovered" {
		t.Fatal(r)
	}
	f.missing("state/shared/rules")
	f.apply()
	f.expect("AGENTS.md", "one")
}
func TestNestedPathsRejected(t *testing.T) {
	f := skillFixture(t)
	f.raw.Resources = append(f.raw.Resources, resourceInput{ID: "nested", Kind: "portable-file", Scope: "project", Claude: "claude-skill/SKILL.md", Codex: "elsewhere"})
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "overlap")
}
func TestResourceRebindingRejected(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.raw.Resources[0].Codex = "other.md"
	f.load()
	_, err := Plan(f.c)
	contains(t, err, "changed identity")
}
func TestNoExtraTransactionsWhenUnchanged(t *testing.T) {
	f := newFixture(t)
	f.apply()
	before, err := os.ReadDir(f.path("state/backups"))
	must(t, err)
	f.apply()
	after, err := os.ReadDir(f.path("state/backups"))
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("extra transaction")
	}
}
func TestHardlinksRejected(t *testing.T) {
	f := newFixture(t)
	must(t, os.Link(f.path("CLAUDE.md"), f.path("AGENTS.md")))
	_, err := Plan(f.c)
	contains(t, err, "hard-linked")
}
func TestReservedIDsRejected(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources[0].ID = "toString"
	f.save()
	_, err := LoadConfig(f.path("config.json"))
	contains(t, err, "path-safe")
}
func TestRecoveryTargetValidation(t *testing.T) {
	f := newFixture(t)
	f.write("unmanaged.txt", "preserve me")
	tx := "11111111-1111-1111-1111-111111111111"
	must(t, writeJSON(pendingPath(f.c), Pending{tx}))
	must(t, writeJSON(f.path("state/backups/"+tx+"/journal.json"), Journal{1, []Operation{{"bad", f.path("unmanaged.txt"), nil, &Snapshot{base64.StdEncoding.EncodeToString([]byte("preserve me")), 0644}}}}))
	_, err := Recover(f.c)
	contains(t, err, "invalid target")
	f.expect("unmanaged.txt", "preserve me")
}

func TestNodeV02ManifestCompatibility(t *testing.T) {
	f := newFixture(t)
	f.apply()
	m := f.plan().Manifest
	// Node property order/whitespace may differ; equality must be semantic.
	raw := map[string]any{"files": m.Files, "resources": m.Resources, "version": 2}
	data, err := json.Marshal(raw)
	must(t, err)
	f.write("state/manifest.json", string(data))
	before := f.read("state/manifest.json")
	f.apply()
	f.expect("state/manifest.json", before)
	f.write("AGENTS.md", "from new runtime")
	f.apply()
	f.expect("CLAUDE.md", "from new runtime")
}

func TestNodeV02FingerprintGolden(t *testing.T) {
	s := &Snapshot{Data: "b25l", Mode: 0644}
	const expected = "cf52d2e42cfa06530d77e87cf6ef88140d69723d3331dba8c63cb6b686f5b522"
	if fingerprint(s) != expected {
		t.Fatal("fingerprint differs from Node v0.2")
	}
}

func TestNodeV02ResourcePropertyOrder(t *testing.T) {
	r := Resource{"rules", "portable-file", "global", map[string]string{"shared": "s", "claude": "c", "codex": "x"}}
	data, err := json.Marshal(r)
	must(t, err)
	const expected = `{"id":"rules","kind":"portable-file","scope":"global","paths":{"shared":"s","claude":"c","codex":"x"}}`
	if string(data) != expected {
		t.Fatalf("Node identity comparison would fail: %s", data)
	}
}
func TestLegacyManifestRejected(t *testing.T) {
	f := newFixture(t)
	f.write("state/manifest.json", `{"rules":"legacy"}`)
	_, err := Plan(f.c)
	contains(t, err, "legacy")
}
func TestRecoveryValidatesSnapshotBeforeAnyRestore(t *testing.T) {
	f := newFixture(t)
	tx := "11111111-1111-1111-1111-111111111111"
	must(t, writeJSON(pendingPath(f.c), Pending{tx}))
	must(t, writeJSON(f.path("state/backups/"+tx+"/journal.json"), Journal{1, []Operation{{"bad", f.path("CLAUDE.md"), &Snapshot{"bad base64", 0600}, &Snapshot{base64.StdEncoding.EncodeToString([]byte("one")), 0644}}}}))
	_, err := Recover(f.c)
	if err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	f.expect("CLAUDE.md", "one")
}
