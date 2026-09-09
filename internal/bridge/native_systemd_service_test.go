package bridge

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Run only as the disposable CI user, never as a developer's real account. The
// workflow creates its temporary home and real user manager on an ephemeral VM.
func TestNativeSystemdServiceLifecycle(t *testing.T) {
	root := nativeSystemdRoot(t)
	runNativeSystemdLifecycle(t, root)
}

func nativeSystemdRoot(t *testing.T) string {
	t.Helper()
	if os.Getenv("AGENT_BRIDGE_SYSTEMD_LIFECYCLE_TESTS") != "1" {
		t.Skip("isolated native systemd lifecycle opt-in required")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native systemd lifecycle requires Linux")
	}
	account, err := user.Current()
	must(t, err)
	root := os.Getenv("AGENT_BRIDGE_SYSTEMD_FIXTURE_ROOT")
	if account.Username != "ab-fixture" || account.Uid == "0" || root != account.HomeDir || filepath.Dir(root) != "/tmp" || !strings.HasPrefix(filepath.Base(root), "agent-bridge-linux.") {
		t.Fatal("refusing native service test outside disposable fixture account")
	}
	if os.Getenv("HOME") != root || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(root, ".config") {
		t.Fatal("fixture home mismatch")
	}
	must(t, assertSafe(root))
	return root
}

func runNativeSystemdLifecycle(t *testing.T, root string) {
	f := newFixture(t)
	if !inside(root, f.dir) {
		t.Fatal("test files must remain in disposable home")
	}
	s, err := NewSystemdService(f.path("config.json"), filepath.Join(root, ".config"))
	must(t, err)
	binary := filepath.Join(root, "agent-bridge")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		if err := s.Uninstall(context.Background(), Systemctl); err != nil {
			t.Errorf("fixture service cleanup: %v", err)
		}
	})
	for _, apply := range []bool{false, true} {
		must(t, s.Install(f.c, binary, apply))
		must(t, s.Start(ctx, Systemctl))
		status, err := s.Status(ctx, Systemctl)
		must(t, err)
		if !status.Installed || status.Registration != "active" || status.Apply != apply {
			t.Fatal(status)
		}
		if !apply {
			time.Sleep(1500 * time.Millisecond)
			f.missing("AGENTS.md")
			must(t, s.Uninstall(ctx, Systemctl))
			continue
		}
		await := func(want string) {
			t.Helper()
			deadline := time.Now().Add(12 * time.Second)
			for time.Now().Before(deadline) {
				data, err := os.ReadFile(f.path("AGENTS.md"))
				if err == nil && string(data) == want {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
			t.Fatal("native systemd watcher did not synchronize fixture")
		}
		await("one")
		must(t, s.Stop(ctx, Systemctl))
		f.write("CLAUDE.md", "two")
		time.Sleep(1500 * time.Millisecond)
		f.expect("AGENTS.md", "one")
		must(t, s.Start(ctx, Systemctl))
		await("two")
		must(t, s.Uninstall(ctx, Systemctl))
		for _, path := range []string{s.unitPath(), s.receiptPath(), s.enablePath()} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("owned service artifact retained", err)
			}
		}
		f.expect("CLAUDE.md", "two")
		f.expect("AGENTS.md", "two")
		if _, err := os.Stat(manifestPath(f.c)); err != nil {
			t.Fatal("uninstall removed sync state", err)
		}
	}
}

// Two separate invocations bracket a real manager restart performed by the CI
// harness. Persistent fixtures are confined to that harness's disposable home.
func TestNativeSystemdManagerRestart(t *testing.T) {
	root := nativeSystemdRoot(t)
	stage := os.Getenv("AGENT_BRIDGE_SYSTEMD_RESTART_STAGE")
	if stage != "prepare" && stage != "verify" {
		t.Fatal("explicit restart stage required")
	}
	f := &fixture{t: t, dir: filepath.Join(root, "restart-fixture")}
	if stage == "prepare" {
		must(t, os.Mkdir(f.dir, 0700))
		f.raw = configInput{Version: 1, StateDir: "state", Resources: []resourceInput{{ID: "rules", Kind: "portable-file", Scope: "global", Claude: "CLAUDE.md", Codex: "AGENTS.md"}}}
		f.write("CLAUDE.md", "one")
		f.load()
	} else {
		var err error
		f.c, err = LoadConfig(f.path("config.json"))
		must(t, err)
	}
	s, err := NewSystemdService(f.path("config.json"), filepath.Join(root, ".config"))
	must(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	binary := filepath.Join(root, "agent-bridge")
	await := func(want string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(f.path("AGENTS.md"))
			if err == nil && string(data) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("restarted watcher did not synchronize fixture")
	}
	if stage == "prepare" {
		must(t, s.Install(f.c, binary, true))
		must(t, s.Start(ctx, Systemctl))
		await("one")
		pid, err := Systemctl(ctx, "show", s.Label, "--property=MainPID", "--value")
		must(t, err)
		if pid == "" || pid == "0" {
			t.Fatal("watcher has no process")
		}
		f.write("previous-pid", pid)
		return // Manager restart must retain the installed unit and sync baseline.
	}
	t.Cleanup(func() { must(t, s.Uninstall(context.Background(), Systemctl)) })
	status, err := s.Status(ctx, Systemctl)
	must(t, err)
	if status.Registration != "active" || !status.Apply {
		t.Fatal("enabled watcher did not return after manager restart", status)
	}
	pid, err := Systemctl(ctx, "show", s.Label, "--property=MainPID", "--value")
	must(t, err)
	if pid == "" || pid == "0" || pid == f.read("previous-pid") {
		t.Fatal("expected a new watcher process")
	}
	f.write("CLAUDE.md", "two")
	await("two")
	must(t, s.Stop(ctx, Systemctl))
	must(t, s.Uninstall(ctx, Systemctl))
	// Both binaries are built from this revision. This tests the replacement
	// procedure and retained state, not arbitrary cross-version migrations.
	must(t, os.Rename(filepath.Join(root, "agent-bridge-next"), binary))
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	must(t, err)
	if strings.TrimSpace(string(out)) != "agent-bridge fixture-v2" {
		t.Fatal("replacement binary was not installed")
	}
	must(t, s.Install(f.c, binary, true))
	must(t, s.Start(ctx, Systemctl))
	f.write("CLAUDE.md", "three")
	await("three")
	must(t, s.Uninstall(ctx, Systemctl))
	f.expect("CLAUDE.md", "three")
	f.expect("AGENTS.md", "three")
	if _, err := os.Stat(manifestPath(f.c)); err != nil {
		t.Fatal("replacement lost synchronization baseline", err)
	}
}
