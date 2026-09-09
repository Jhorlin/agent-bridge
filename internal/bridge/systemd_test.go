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

func TestSystemdUnit(t *testing.T) {
	f := newFixture(t)
	binary := f.path("bridge binary")
	f.write("bridge binary", "inert test executable")
	must(t, os.Chmod(binary, 0700))
	for _, apply := range []bool{false, true} {
		unit, err := SystemdUnit(f.path("config.json"), binary, apply)
		must(t, err)
		if strings.Contains(unit, " --apply") != apply || !strings.Contains(unit, "Restart=no") || !strings.Contains(unit, "TimeoutStopSec=infinity") {
			t.Fatal(unit)
		}
		f.missing("state")
		f.missing("AGENTS.md")
	}
	f.write("AGENTS.md", "conflict")
	if _, err := SystemdUnit(f.path("config.json"), binary, true); err == nil {
		t.Fatal("applying conflict accepted")
	}
	if _, err := SystemdUnit(f.path("config.json"), binary, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"relative", "/newline\nfile", binary + "%", binary + "$"} {
		if _, err := SystemdUnit(f.path("config.json"), bad, false); err == nil {
			t.Fatal("unsafe binary accepted")
		}
	}
	must(t, os.Symlink(binary, f.path("linked-binary")))
	if _, err := SystemdUnit(f.path("config.json"), f.path("linked-binary"), false); err == nil {
		t.Fatal("symlink executable accepted")
	}
	must(t, os.Chmod(binary, 0600))
	if _, err := SystemdUnit(f.path("config.json"), binary, false); err == nil {
		t.Fatal("non-executable accepted")
	}
}

func TestSystemdQuote(t *testing.T) {
	input := `/tmp/a "quote"\slash/$VAR/%h/config.json`
	if got := systemdQuote(input, true); got != `"/tmp/a \"quote\"\\slash/$$VAR/%%h/config.json"` {
		t.Fatal(got)
	}
	if got := systemdQuote(input, false); strings.Contains(got, "$$") || !strings.Contains(got, "%%h") {
		t.Fatal(got)
	}
}

// Native parser validation only: never installs, starts or enables a service.
func TestNativeSystemdUnitVerify(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_SYSTEMD_TESTS") != "1" {
		t.Skip("opt-in systemd parser test")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native systemd validation requires Linux")
	}
	analyze, err := exec.LookPath("systemd-analyze")
	must(t, err)
	f := newFixture(t)
	f.write(`space %h $VAR "quoted"/profile.json`, f.read("config.json"))
	binary, err := os.Executable()
	must(t, err)
	for _, apply := range []bool{false, true} {
		// Empty profile isolates service syntax from adapter semantics.
		f.write(`space %h $VAR "quoted"/profile.json`, `{"version":1,"stateDir":"state","resources":[]}`)
		unit, err := SystemdUnit(f.path(`space %h $VAR "quoted"/profile.json`), binary, apply)
		must(t, err)
		file := filepath.Join(f.dir, "agent-bridge-test.service")
		must(t, os.WriteFile(file, []byte(unit), 0600))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, analyze, "verify", "--man=no", file)
		cmd.Env = append(os.Environ(), "SYSTEMD_OFFLINE=1")
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil || len(output) > 0 {
			t.Fatalf("systemd validation: %v %s", err, output)
		}
	}
}
