package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

func diagnosticWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// Rejecting malformed options before inspection prevents accidental bundle
// creation and avoids making service queries for an invalid command.
func TestDiagnosticBoundaryInvalidOptionsHaveNoSideEffects(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	before := tree(t, dir)
	for _, args := range [][]string{
		{"doctor"}, {"doctor", profile, "extra"}, {"logs", profile, "--tail"},
		{"logs", profile, "--tail", "zero"}, {"logs", profile, "--tail", "0"},
		{"logs", profile, "--tail", "1001"}, {"logs", profile, "--unknown", "5"},
		{"support-bundle", profile}, {"unknown", profile},
	} {
		var out, warnings bytes.Buffer
		service := func(context.Context, string) diagnosticService {
			t.Fatal("invalid diagnostic command queried a service")
			return diagnosticService{}
		}
		if code := runDiagnosticsWith(context.Background(), args, &out, &warnings, service); code != 1 || out.Len() != 0 || warnings.Len() == 0 {
			t.Fatalf("invalid diagnostic command did not fail clearly: %v code=%d", args, code)
		}
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("invalid diagnostics changed files")
	}
}

func TestDiagnosticBoundaryOutputFailuresDoNotClaimSuccess(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	before := tree(t, dir)
	for _, command := range []string{"doctor", "logs"} {
		if code := runDiagnosticsWith(context.Background(), []string{command, profile}, &auditFailWriter{}, io.Discard, fakeStatus); code != 1 {
			t.Fatal("diagnostic output failure reported success", command)
		}
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("failed read-only output changed files")
	}
	// Bundle creation precedes its acknowledgment: a broken output must report
	// failure, but the successfully created, sanitized artifact remains usable.
	target := filepath.Join(dir, "support.json")
	if code := runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, target}, &auditFailWriter{}, io.Discard, fakeStatus); code != 1 {
		t.Fatal("bundle acknowledgment failure reported success")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var report diagnosticReport
	if json.Unmarshal(data, &report) != nil || report.Schema != 1 || strings.Contains(string(data), dir) {
		t.Fatal("acknowledgment failure lost or disclosed the bundle")
	}
	after := tree(t, dir)
	delete(after, target)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("bundle acknowledgment failure altered other files")
	}
}

func TestDiagnosticBoundaryUnsafeStateAndCoordinationMetadata(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	diagnosticWrite(t, profile, `{"version":1,"stateDir":"state","coordinationDir":"coordination","resources":[{"id":"rules","kind":"portable-file","scope":"global","claude":"CLAUDE.md","codex":"AGENTS.md"}]}`)
	diagnosticWrite(t, filepath.Join(dir, "private-pending"), "PRIVATE_PENDING_CONTENT")
	if err := os.MkdirAll(filepath.Join(dir, "state", "file-change-pending.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "private-pending"), filepath.Join(dir, "state", "pending.json")); err != nil {
		t.Fatal(err)
	}
	diagnosticWrite(t, filepath.Join(dir, "coordination", "sync.lock"), "PRIVATE_LOCK_CONTENT")
	diagnosticWrite(t, filepath.Join(dir, "coordination", "enrollment-pending.json"), "PRIVATE_PENDING_CONTENT")
	before := tree(t, dir)
	var out bytes.Buffer
	if code := runDiagnosticsWith(context.Background(), []string{"doctor", profile}, &out, io.Discard, fakeStatus); code != 0 {
		t.Fatal("doctor must remain available for unsafe state", code)
	}
	var report diagnosticReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"syncPending": "unsafe", "fileChangePending": "unsafe", "syncLock": "absent", "coordinationLock": "present", "enrollmentPending": "present"} {
		if report.State[key] != want {
			t.Fatalf("state %s = %s, want %s", key, report.State[key], want)
		}
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) || !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("state diagnostics read private contents or changed artifacts")
	}
}

func TestDiagnosticBoundaryProtectsIndirectManagedPaths(t *testing.T) {
	for _, kind := range []string{"instruction-companion", "agent-export", "pinned-link", "coordination"} {
		t.Run(kind, func(t *testing.T) {
			dir, profile := diagnosticFixture(t)
			var resource map[string]any
			var targets []string
			switch kind {
			case "instruction-companion":
				resource = map[string]any{"id": "rules", "kind": "instruction-set", "scope": "project", "portable": true, "claude": "CLAUDE.md", "codex": "AGENTS.md"}
				targets = []string{filepath.Join(dir, ".claude", "CLAUDE.md")}
			case "agent-export":
				resource = map[string]any{"id": "bundle", "kind": "plugin-directory", "scope": "project", "portable": true, "allowReformat": true, "claude": "claude-plugin", "codex": "codex-plugin", "codexAgentExports": map[string]string{"reviewer": "exports/bridge-bundle-reviewer.toml"}}
				diagnosticWrite(t, filepath.Join(dir, "claude-plugin", ".claude-plugin", "plugin.json"), `{"name":"bundle"}`)
				diagnosticWrite(t, filepath.Join(dir, "claude-plugin", "agents", "reviewer.md"), "---\nname: reviewer\ndescription: Review fixture.\n---\nReview only.\n")
				targets = []string{filepath.Join(dir, "exports", "bridge-bundle-reviewer.toml")}
			case "pinned-link":
				resource = map[string]any{"id": "rules", "kind": "portable-file", "scope": "global", "claude": "linked.md", "codex": "AGENTS.md", "linkTargets": map[string]string{"claude": "CLAUDE.md"}}
				if err := os.Symlink(filepath.Join(dir, "CLAUDE.md"), filepath.Join(dir, "linked.md")); err != nil {
					t.Fatal(err)
				}
				targets = []string{filepath.Join(dir, "linked.md"), filepath.Join(dir, "CLAUDE.md")}
			case "coordination":
				resource = map[string]any{"id": "rules", "kind": "portable-file", "scope": "global", "claude": "CLAUDE.md", "codex": "AGENTS.md"}
				targets = []string{filepath.Join(dir, "coordination", "support.json")}
			}
			config := map[string]any{"version": 1, "stateDir": "state", "resources": []any{resource}}
			if kind == "coordination" {
				config["coordinationDir"] = "coordination"
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			diagnosticWrite(t, profile, string(data))
			if _, err := bridge.LoadAuditConfig(profile); err != nil {
				t.Fatal("fixture is not a supported profile", err)
			}
			before := tree(t, dir)
			for _, target := range targets {
				var warnings bytes.Buffer
				if code := runDiagnosticsWith(context.Background(), []string{"support-bundle", profile, target}, io.Discard, &warnings, fakeStatus); code != 1 || !strings.Contains(warnings.String(), "overlaps protected") {
					t.Fatalf("indirect managed path was not protected: %s code=%d warning=%s", kind, code, &warnings)
				}
			}
			if !reflect.DeepEqual(before, tree(t, dir)) {
				t.Fatal("rejected bundle changed protected data")
			}
		})
	}
}

func TestDiagnosticBoundaryUnavailableServiceStaysPrivate(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("supported service platforms only")
	}
	for _, kind := range []string{"linked-home", "unowned-service"} {
		t.Run(kind, func(t *testing.T) {
			dir, profile := diagnosticFixture(t)
			home := os.Getenv("HOME")
			if kind == "linked-home" {
				alias := filepath.Join(dir, "home-alias")
				if err := os.Symlink(home, alias); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", alias)
				if runtime.GOOS == "linux" {
					t.Setenv("XDG_CONFIG_HOME", filepath.Join(alias, "config"))
				}
			} else if runtime.GOOS == "darwin" {
				s, err := bridge.NewLaunchService(profile, home, os.Getuid())
				if err != nil {
					t.Fatal(err)
				}
				diagnosticWrite(t, filepath.Join(s.Root, s.Label+".plist"), "PRIVATE_UNOWNED_SERVICE")
			} else {
				s, err := bridge.NewSystemdService(profile, os.Getenv("XDG_CONFIG_HOME"))
				if err != nil {
					t.Fatal(err)
				}
				diagnosticWrite(t, filepath.Join(s.Root, s.Label), "PRIVATE_UNOWNED_SERVICE")
			}
			// Both inputs fail before any launchctl/systemctl invocation. The
			// actual service adapter is exercised, not a fabricated status.
			before := tree(t, dir)
			var out bytes.Buffer
			if code := Run(context.Background(), []string{"doctor", profile}, &out, io.Discard); code != 0 {
				t.Fatal("doctor unavailable when only service inspection failed", code)
			}
			var report diagnosticReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Service.Status != "unavailable" || report.Service.Installed != nil || report.Service.Apply != nil || strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) {
				t.Fatal("unavailable service produced a false status or leaked private data")
			}
			if !reflect.DeepEqual(before, tree(t, dir)) {
				t.Fatal("doctor altered service artifacts")
			}
		})
	}
}

func TestDiagnosticBoundaryMissingHomeDoesNotPreventSync(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	t.Setenv("HOME", "")
	var warnings bytes.Buffer
	if code := RunLogged(context.Background(), []string{"sync", profile}, io.Discard, &warnings); code != 0 || !strings.Contains(warnings.String(), "log_path_unsafe") {
		t.Fatalf("unavailable log home prevented sync: code=%d warning=%s", code, &warnings)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || string(data) != "one" {
		t.Fatal("sync did not complete without logging")
	}
	var out bytes.Buffer
	before := tree(t, dir)
	if code := Run(context.Background(), []string{"doctor", profile}, &out, &warnings); code != 1 || out.Len() != 0 {
		t.Fatal("doctor claimed a report without a diagnostic home")
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("failed diagnostic location lookup wrote files")
	}
}

func TestDiagnosticBoundaryServiceAndEnrollmentLogRouting(t *testing.T) {
	dir, profile := diagnosticFixture(t)
	// An absent service stops without invoking the native manager.
	if code := RunLogged(context.Background(), []string{"service", "stop", profile}, io.Discard, io.Discard); code != 0 {
		t.Fatal("stopping absent disposable service failed", code)
	}
	logDir, err := logDirectory(profile)
	if err != nil {
		t.Fatal(err)
	}
	read := diagnostics.Read(logDir, 50)
	if len(read.Events) == 0 || read.Events[0].Command != "service-stop" || read.Events[0].Profile != diagnostics.Ref(profile) {
		t.Fatal("service operation log was associated with its action instead of profile")
	}
	before := tree(t, dir)
	if code := RunLogged(context.Background(), []string{"service", "status", profile}, io.Discard, io.Discard); code != 0 {
		t.Fatal("absent disposable service status failed", code)
	}
	if !reflect.DeepEqual(before, tree(t, dir)) {
		t.Fatal("service status appended a mutating-command log")
	}
	target := filepath.Join(dir, "new-profile.json")
	if code := RunLogged(context.Background(), []string{"create-enrolled", profile, target, strings.Repeat("0", 64)}, io.Discard, io.Discard); code != 1 {
		t.Fatal("template without coordinator unexpectedly enrolled", code)
	}
	targetLog, err := logDirectory(target)
	if err != nil {
		t.Fatal(err)
	}
	read = diagnostics.Read(targetLog, 50)
	if len(read.Events) == 0 || read.Events[0].Command != "create-enrolled" || read.Events[0].Profile != diagnostics.Ref(target) {
		t.Fatal("enrollment diagnostic was not associated with target profile")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("failed enrollment created a profile")
	}
}
