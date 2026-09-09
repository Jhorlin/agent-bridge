package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

func diagnosticFixture(t *testing.T) (string, string) {
	t.Helper()
	dir, profile := setup(t)
	home := filepath.Join(dir, "isolated-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("CANARY_ENV", "CANARY_ENV_VALUE")
	return dir, profile
}
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		fi, e := d.Info()
		if e != nil {
			return e
		}
		result[p] = fi.Mode().String()
		if fi.Mode().IsRegular() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			result[p] += string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func fakeStatus(context.Context, string) diagnosticService {
	return diagnosticService{Status: "not-installed"}
}

func TestLoggedSyncDoctorAndSanitizedBundle(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	b, e := os.ReadFile(profile)
	if e != nil {
		t.Fatal(e)
	}
	b = bytes.ReplaceAll(b, []byte(`"rules"`), []byte(`"CANARY_RESOURCE"`))
	if e := os.WriteFile(profile, b, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("CANARY_NATIVE_TOKEN"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, warnings bytes.Buffer
	if code := RunLogged(context.Background(), []string{"sync", profile}, &out, &warnings); code != 0 || warnings.Len() != 0 {
		t.Fatalf("sync %d %s", code, &warnings)
	}
	logDir, e := logDirectory(profile)
	if e != nil {
		t.Fatal(e)
	}
	r := diagnostics.Read(logDir, 100)
	if len(r.Events) < 5 || r.Rejected != 0 {
		t.Fatalf("missing events %+v", r)
	}
	for _, event := range r.Events {
		if event.Run != r.Events[0].Run {
			t.Fatal("run split")
		}
	}
	before := tree(t, dir)
	out.Reset()
	if code := runDiagnosticsWith(context.Background(), []string{"doctor", profile}, &out, io.Discard, fakeStatus); code != 0 {
		t.Fatal(code)
	}
	var doctor diagnosticReport
	if e := json.Unmarshal(out.Bytes(), &doctor); e != nil {
		t.Fatal(e)
	}
	if doctor.Config != "ok" || doctor.Plan != "ok" || doctor.State["syncPending"] != "absent" || doctor.State["manifest"] != "present" {
		t.Fatalf("wrong report %+v", doctor)
	}
	if strings.Contains(out.String(), "CANARY") || strings.Contains(out.String(), dir) {
		t.Fatal("doctor leaked data")
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("doctor changed files")
	}
	out.Reset()
	if runDiagnosticsWith(context.Background(), []string{"logs", profile, "--tail", "4"}, &out, io.Discard, fakeStatus) != 0 {
		t.Fatal("logs failed")
	}
	if !strings.Contains(out.String(), "CANARY_RESOURCE") || !strings.Contains(out.String(), logDir) || strings.Contains(out.String(), "CANARY_NATIVE_TOKEN") {
		t.Fatal("local mapping incorrect")
	}
	output := filepath.Join(dir, "support.json")
	out.Reset()
	if runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, output}, &out, io.Discard, fakeStatus) != 0 {
		t.Fatal("bundle failed")
	}
	b, e = os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("CANARY")) || bytes.Contains(b, []byte(dir)) || bytes.Contains(b, []byte("localReferences")) || bytes.Contains(b, []byte("journal.json")) {
		t.Fatal("bundle leaked data")
	}
	if e := json.Unmarshal(b, &doctor); e != nil {
		t.Fatal(e)
	}
	after := tree(t, dir)
	delete(after, output)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("bundle altered other files")
	}
	if runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, output}, io.Discard, io.Discard, fakeStatus) != 1 {
		t.Fatal("overwrote bundle")
	}
}
func TestLoggedReadOnlyCommandsCreateNoLogs(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	before := tree(t, dir)
	for _, args := range [][]string{{"plan", profile}, {"audit", profile, "--json"}, {"config", profile}, {"doctor", profile}, {"logs", profile}, {"version"}} {
		if code := RunLogged(context.Background(), args, io.Discard, io.Discard); code != 0 {
			t.Fatalf("%v exit %d", args, code)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if code := RunLogged(ctx, []string{"watch", profile}, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("readonly command wrote files")
	}
}
func TestLoggingFailureDoesNotPreventSync(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	logDir, e := logDirectory(profile)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(logDir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(logDir, 0755); e != nil {
		t.Fatal(e)
	}
	var warning bytes.Buffer
	if code := RunLogged(context.Background(), []string{"sync", profile}, io.Discard, &warning); code != 0 {
		t.Fatal("logging failure aborted sync", code)
	}
	b, e := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if e != nil || string(b) != "one" {
		t.Fatal("sync did not complete")
	}
	if strings.Count(warning.String(), "log_write_failed") != 1 {
		t.Fatalf("wrong warning %s", &warning)
	}
}
func TestLoggedConfigurationFailureAndPendingState(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	if e := os.WriteFile(profile, []byte(`{"CANARY_CONFIG":"CANARY_VALUE"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if RunLogged(context.Background(), []string{"sync", profile}, io.Discard, io.Discard) != 1 {
		t.Fatal("invalid config accepted")
	}
	logDir, _ := logDirectory(profile)
	r := diagnostics.Read(logDir, 50)
	found := false
	for _, event := range r.Events {
		if event.Stage == "config_load" && event.Code != "ok" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing load failure", r)
	}
	var out bytes.Buffer
	if runDiagnosticsWith(context.Background(), []string{"doctor", profile}, &out, io.Discard, fakeStatus) != 0 || strings.Contains(out.String(), "CANARY") {
		t.Fatal("unsafe invalid profile report")
	}
	if runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, filepath.Join(dir, "bundle.json")}, io.Discard, io.Discard, fakeStatus) != 1 {
		t.Fatal("cannot protect invalid profile output")
	}
	// A valid profile can report pending artifacts without reading their contents.
	dir, profile = setup(t)
	c, e := bridge.LoadConfig(profile)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(c.StateDir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"pending.json", "file-change-pending.json", "sync.lock"} {
		if e := os.WriteFile(filepath.Join(c.StateDir, name), []byte("CANARY_SNAPSHOT"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	out.Reset()
	if runDiagnosticsWith(context.Background(), []string{"doctor", profile}, &out, io.Discard, fakeStatus) != 0 {
		t.Fatal("doctor failed on pending")
	}
	var report diagnosticReport
	_ = json.Unmarshal(out.Bytes(), &report)
	if report.State["syncPending"] != "present" || report.State["syncLock"] != "present" || report.State["fileChangePending"] != "present" || strings.Contains(out.String(), "CANARY") {
		t.Fatal("pending diagnostics incorrect")
	}
}
func TestBundleProtectsManagedAndLinkedDestinations(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	logDir, _ := logDirectory(profile)
	for _, target := range []string{profile, filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "state", "bundle.json"), filepath.Join(logDir, "bundle.json")} {
		before := tree(t, dir)
		if runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, target}, io.Discard, io.Discard, fakeStatus) != 1 {
			t.Fatal("unsafe output accepted", target)
		}
		if !reflect.DeepEqual(before, tree(t, dir)) {
			t.Fatal("unsafe output changed files")
		}
	}
	if e := os.Symlink(filepath.Join(dir, "CLAUDE.md"), filepath.Join(dir, "linked.json")); e != nil {
		t.Fatal(e)
	}
	if runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, filepath.Join(dir, "linked.json")}, io.Discard, io.Discard, fakeStatus) != 1 {
		t.Fatal("symlink output accepted")
	}
}
func TestConflictAndWatchTransitionsAreLogged(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	if e := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("CANARY_CONFLICT"), 0600); e != nil {
		t.Fatal(e)
	}
	if RunLogged(context.Background(), []string{"sync", profile}, io.Discard, io.Discard) != 2 {
		t.Fatal("conflict exit")
	}
	logDir, _ := logDirectory(profile)
	r := diagnostics.Read(logDir, 50)
	if len(r.Events) != 3 || r.Events[1].Stage != "conflict" || r.Events[1].Code != "conflict" {
		t.Fatal("conflict event", r)
	}
	if e := os.WriteFile(profile, []byte("CANARY_BROKEN"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()
	if RunLogged(ctx, []string{"watch", profile, "--apply"}, io.Discard, io.Discard) != 0 {
		t.Fatal("watch shutdown")
	}
	r = diagnostics.Read(logDir, 50)
	paused := 0
	for _, e := range r.Events {
		if e.Stage == "watch_paused" {
			paused++
		}
	}
	if paused != 1 {
		t.Fatal("watch failures flooded log", r)
	}
}

func TestLoggingNeverEntersManagedState(t *testing.T) {
	_, profile := diagnosticFixture(t)
	logDir, _ := logDirectory(profile)
	b, e := os.ReadFile(profile)
	if e != nil {
		t.Fatal(e)
	}
	var config map[string]any
	if e := json.Unmarshal(b, &config); e != nil {
		t.Fatal(e)
	}
	config["stateDir"] = logDir
	b, _ = json.Marshal(config)
	if e := os.WriteFile(profile, b, 0600); e != nil {
		t.Fatal(e)
	}
	var warning bytes.Buffer
	if code := RunLogged(context.Background(), []string{"sync", profile}, io.Discard, &warning); code != 0 || !strings.Contains(warning.String(), "log_path_unsafe") {
		t.Fatalf("overlap not protected %d %s", code, &warning)
	}
	if _, e := os.Lstat(filepath.Join(logDir, "events.jsonl")); !os.IsNotExist(e) {
		t.Fatal("logs wrote managed state")
	}
	if !overlaps("/", logDir) || overlaps(filepath.Join(logDir, "a"), filepath.Join(logDir, "ab")) {
		t.Fatal("path boundaries incorrect")
	}
}

func TestDoctorReportsServiceModeAndNoWrites(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	before := tree(t, dir)
	called := false
	var out bytes.Buffer
	service := func(ctx context.Context, p string) diagnosticService {
		called = true
		if p != profile || ctx.Err() != nil {
			t.Fatal("wrong service query")
		}
		installed, apply := true, true
		return diagnosticService{Status: "active", Installed: &installed, Apply: &apply}
	}
	if runDiagnosticsWith(context.Background(), []string{"doctor", profile}, &out, io.Discard, service) != 0 || !called {
		t.Fatal("service not inspected")
	}
	var report diagnosticReport
	if e := json.Unmarshal(out.Bytes(), &report); e != nil {
		t.Fatal(e)
	}
	if report.Service.Status != "active" || report.Service.Apply == nil || !*report.Service.Apply || !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("incorrect service diagnostics")
	}
}
