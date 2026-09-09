package bridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Explicitly opt in: registers only a unique temporary fixture in the current
// GUI domain, never a plist or profile in the real home. No model calls occur.
func TestNativeLaunchService(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_SERVICE_TESTS") != "1" || runtime.GOOS != "darwin" {
		t.Skip("set AGENT_BRIDGE_SERVICE_TESTS=1 on macOS to test isolated launchd lifecycle")
	}
	f, _, binary := serviceFixture(t)
	s, err := NewLaunchService(f.path("config.json"), f.path("home"), os.Getuid())
	must(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	code, err := Launchctl(ctx, "print", s.Domain)
	if err != nil || code != 0 {
		t.Skip("no accessible GUI launchd domain")
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/agent-bridge")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	// launchd does not inherit the test process's HOME. A fixture-only wrapper
	// confines new runtime diagnostics to the disposable home, then execs the
	// real compiled binary. Never write test logs into the operator's home.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	wrapper := f.path("fixture-launch")
	must(t, os.WriteFile(wrapper, []byte("#!/bin/sh\nexec /usr/bin/env HOME="+quote(f.path("home"))+" "+quote(binary)+" \"$@\"\n"), 0700))
	binary = wrapper
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		// Exact fixture label, including on a failed assertion. Do not rely on
		// receipt validation to stop a test process during cleanup.
		registered, err := s.registration(cleanup, Launchctl)
		if err != nil {
			t.Errorf("fixture registration cleanup: %v", err)
			return
		}
		if registered {
			code, err := Launchctl(cleanup, "bootout", "--wait", s.target())
			if err != nil || code != 0 {
				t.Errorf("fixture cleanup failed: code %d, %v", code, err)
			}
		}
	})
	waitPeer := func(want string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(f.path("AGENTS.md"))
			if string(data) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("fixture watcher did not synchronize before deadline")
	}
	must(t, s.Install(f.c, binary, true))
	must(t, s.Start(ctx, Launchctl))
	waitPeer("one")
	status, err := s.Status(ctx, Launchctl)
	must(t, err)
	if status.Registration != "registered" {
		t.Fatal(status)
	}
	must(t, s.Stop(ctx, Launchctl))
	f.missing("state/sync.lock")
	f.write("CLAUDE.md", "two")
	must(t, s.Start(ctx, Launchctl))
	waitPeer("two")
	must(t, s.Uninstall(ctx, Launchctl))
	f.missing("state/sync.lock")
	f.expect("CLAUDE.md", "two")
	f.expect("AGENTS.md", "two")
	for _, path := range []string{manifestPath(f.c), filepath.Join(s.metadataDir(), s.Label+".out.log")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("service removal lost persistent data")
		}
	}
	status, err = s.Status(ctx, Launchctl)
	must(t, err)
	if status.Installed {
		t.Fatal("service still installed")
	}
}
