package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceCLIValidation(t *testing.T) {
	for _, args := range [][]string{{"service"}, {"service", "unknown", "profile"}, {"service", "stop", "profile", "--apply"}, {"service", "install", "profile", "--force"}} {
		var out, errors bytes.Buffer
		if Run(context.Background(), args, &out, &errors) != 1 || !strings.Contains(errors.String(), "Usage:") {
			t.Fatalf("bad validation for %v: %s", args, errors.String())
		}
	}
}

func TestServiceCLIAbsentDoesNotWrite(t *testing.T) {
	dir, profile := setup(t)
	t.Setenv("HOME", filepath.Join(dir, "isolated-home"))
	for _, action := range []string{"status", "stop", "uninstall"} {
		var out, errors bytes.Buffer
		code := Run(context.Background(), []string{"service", action, profile}, &out, &errors)
		if runtime.GOOS == "darwin" && code != 0 {
			t.Fatal(errors.String())
		}
		if runtime.GOOS != "darwin" && code != 1 {
			t.Fatal("accepted unsupported OS")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "isolated-home")); !os.IsNotExist(err) {
		t.Fatal("absent service created home")
	}
}

func TestServiceCLIInstallLifecycleAndFailures(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS service CLI")
	}
	dir, profile := setup(t)
	t.Setenv("HOME", filepath.Join(dir, "isolated-home"))
	registered, fail := false, false
	run := func(_ context.Context, args ...string) (int, error) {
		if fail {
			return 5, nil
		}
		switch args[0] {
		case "print":
			if registered {
				return 0, nil
			}
			return 113, nil
		case "bootstrap", "kickstart":
			registered = true
		case "bootout":
			registered = false
		default:
			t.Fatal(args)
		}
		return 0, nil
	}
	call := func(want int, args ...string) string {
		t.Helper()
		var out, errors bytes.Buffer
		if got := runServiceWith(context.Background(), args, &out, &errors, run); got != want {
			t.Fatalf("%v: got %d want %d: %s", args, got, want, errors.String())
		}
		return out.String()
	}
	call(1, "install", filepath.Join(dir, "absent.json"))
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	call(1, "install", profile, "--apply")
	call(0, "install", profile)
	call(1, "install", profile)
	if result := call(0, "status", profile); !strings.Contains(result, `"apply":false`) {
		t.Fatal(result)
	}
	fail = true
	call(1, "status", profile)
	call(1, "start", profile)
	call(1, "uninstall", profile)
	fail = false
	call(0, "start", profile)
	call(0, "stop", profile)
	call(0, "uninstall", profile)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	call(0, "install", profile, "--apply")
	if result := call(0, "status", profile); !strings.Contains(result, `"apply":true`) {
		t.Fatal(result)
	}
	call(0, "uninstall", profile)
}
