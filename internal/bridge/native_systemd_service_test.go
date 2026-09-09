package bridge

import (
	"context"
	"os"
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
