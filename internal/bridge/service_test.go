package bridge

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func serviceFixture(t *testing.T) (*fixture, LaunchService, string) {
	t.Helper()
	f := newFixture(t)
	s, err := NewLaunchService(f.path("config.json"), f.path("home"), 501)
	must(t, err)
	f.write("bin/agent-bridge", "fixture")
	binary := f.path("bin/agent-bridge")
	must(t, os.Chmod(binary, 0700))
	return f, s, binary
}

func TestServiceLifecycle(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(fmt.Sprint(apply), func(t *testing.T) {
			f, s, binary := serviceFixture(t)
			ctx := context.Background()
			registered := false
			var actions []string
			run := func(_ context.Context, args ...string) (int, error) {
				actions = append(actions, strings.Join(args, " "))
				switch args[0] {
				case "print":
					if registered {
						return 0, nil
					}
					return 113, nil
				case "bootstrap":
					if !reflect.DeepEqual(args, []string{"bootstrap", s.Domain, s.plistPath()}) {
						t.Fatalf("bad bootstrap: %v", args)
					}
					registered = true
				case "kickstart":
					if !reflect.DeepEqual(args, []string{"kickstart", s.target()}) {
						t.Fatalf("bad kickstart: %v", args)
					}
				case "bootout":
					if !reflect.DeepEqual(args, []string{"bootout", "--wait", s.target()}) {
						t.Fatalf("bad bootout: %v", args)
					}
					registered = false
				default:
					t.Fatalf("unexpected operation: %v", args)
				}
				return 0, nil
			}
			status, err := s.Status(ctx, run)
			must(t, err)
			if status.Installed || len(actions) != 0 {
				t.Fatal("absent status must be read-only")
			}
			must(t, s.Stop(ctx, run))
			must(t, s.Uninstall(ctx, run))
			if _, err := os.Stat(s.Root); !os.IsNotExist(err) {
				t.Fatal("absent operations created directories")
			}
			if s.Start(ctx, run) == nil {
				t.Fatal("started absent service")
			}
			must(t, s.Install(f.c, binary, apply))
			if s.Install(f.c, binary, apply) == nil {
				t.Fatal("overwrote installation")
			}
			for _, path := range []string{s.plistPath(), s.receiptPath(), filepath.Join(s.metadataDir(), s.Label+".out.log")} {
				info, err := os.Stat(path)
				must(t, err)
				if info.Mode().Perm() != 0600 {
					t.Fatalf("nonprivate file: %s", path)
				}
			}
			status, err = s.Status(ctx, run)
			must(t, err)
			if !status.Installed || status.Apply != apply || status.Registration != "unregistered" {
				t.Fatalf("bad status: %+v", status)
			}
			must(t, s.Start(ctx, run))
			must(t, s.Start(ctx, run))
			status, err = s.Status(ctx, run)
			must(t, err)
			if status.Registration != "registered" {
				t.Fatal(status)
			}
			must(t, s.Stop(ctx, run))
			must(t, s.Stop(ctx, run))
			must(t, s.Start(ctx, run))
			f.apply()
			before, err := snapshot(filepath.Join(f.c.StateDir, "manifest.json"))
			must(t, err)
			must(t, s.Uninstall(ctx, run))
			must(t, s.Uninstall(ctx, run))
			for _, path := range []string{s.plistPath(), s.receiptPath()} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("owned file remains")
				}
			}
			f.expect("CLAUDE.md", "one")
			f.expect("AGENTS.md", "one")
			after, err := snapshot(filepath.Join(f.c.StateDir, "manifest.json"))
			must(t, err)
			if !equal(before, after) {
				t.Fatal("uninstall changed state")
			}
			if _, err := os.Stat(filepath.Join(s.metadataDir(), s.Label+".out.log")); err != nil {
				t.Fatal("uninstall removed logs")
			}
		})
	}
}

func TestServicePlistEscapesAndRoundTrips(t *testing.T) {
	f, _, _ := serviceFixture(t)
	s, err := NewLaunchService(f.path("a & <b>.json"), f.path("home & data"), 42)
	must(t, err)
	for _, apply := range []bool{false, true} {
		p, err := s.render(f.path("bin & <bridge>"), apply)
		must(t, err)
		raw, err := snapshotBytes(p)
		must(t, err)
		d := xml.NewDecoder(strings.NewReader(string(raw)))
		for {
			_, err := d.Token()
			if err == io.EOF {
				break
			}
			must(t, err)
		}
		if strings.Contains(string(raw), "--apply") != apply {
			t.Fatal("incorrect write opt-in")
		}
		if !strings.Contains(string(raw), "&amp;") || strings.Contains(string(raw), "EnvironmentVariables") {
			t.Fatal("bad plist escaping/environment")
		}
	}
}

func TestServiceRefusesChangedOrUnsafeFiles(t *testing.T) {
	for _, change := range []string{"plist", "receipt", "log-symlink", "log-hardlink", "log-public", "missing-plist"} {
		t.Run(change, func(t *testing.T) {
			f, s, binary := serviceFixture(t)
			must(t, s.Install(f.c, binary, false))
			log := filepath.Join(s.metadataDir(), s.Label+".out.log")
			switch change {
			case "plist":
				must(t, os.WriteFile(s.plistPath(), []byte("changed"), 0600))
			case "receipt":
				must(t, os.WriteFile(s.receiptPath(), []byte("{}"), 0600))
			case "missing-plist":
				must(t, os.Remove(s.plistPath()))
			case "log-symlink":
				must(t, os.Remove(log))
				must(t, os.Symlink(f.path("CLAUDE.md"), log))
			case "log-hardlink":
				must(t, os.Remove(log))
				must(t, os.Link(f.path("CLAUDE.md"), log))
			case "log-public":
				must(t, os.Chmod(log, 0644))
			}
			run := func(_ context.Context, args ...string) (int, error) {
				if args[0] != "print" {
					t.Fatal("unsafe service started")
				}
				return 113, nil
			}
			if s.Start(context.Background(), run) == nil {
				t.Fatal("accepted unsafe file")
			}
			f.expect("CLAUDE.md", "one")
		})
	}
}

func TestServiceRemovalFailuresPreserveFiles(t *testing.T) {
	for _, failure := range []string{"unavailable", "stop", "still-registered", "changed"} {
		t.Run(failure, func(t *testing.T) {
			f, s, binary := serviceFixture(t)
			must(t, s.Install(f.c, binary, true))
			stopped := false
			run := func(_ context.Context, args ...string) (int, error) {
				if args[0] == "print" {
					if failure == "unavailable" {
						return 5, nil
					}
					if stopped && failure != "still-registered" {
						return 113, nil
					}
					return 0, nil
				}
				if args[0] != "bootout" {
					t.Fatal(args)
				}
				if failure == "stop" {
					return 5, nil
				}
				stopped = true
				if failure == "changed" {
					must(t, os.WriteFile(s.plistPath(), []byte("external edit"), 0600))
				}
				return 0, nil
			}
			if s.Uninstall(context.Background(), run) == nil {
				t.Fatal("removed after failure")
			}
			for _, path := range []string{s.plistPath(), s.receiptPath()} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("removed evidence")
				}
			}
		})
	}
}

func TestServiceInstallRejectsOverlapAndNonExecutable(t *testing.T) {
	f, s, binary := serviceFixture(t)
	must(t, os.Chmod(binary, 0600))
	if s.Install(f.c, binary, false) == nil {
		t.Fatal("accepted nonexecutable")
	}
	must(t, os.Chmod(binary, 0700))
	f.c.StateDir = s.Root
	if s.Install(f.c, binary, false) == nil {
		t.Fatal("accepted state overlap")
	}
}

func TestServiceInvalidPathsAndLocks(t *testing.T) {
	f, s, binary := serviceFixture(t)
	for _, path := range []string{"bad\x01path", "bad\npath", "bad\xffpath", "bad\ufffepath"} {
		if _, err := NewLaunchService(f.path(path), f.path("home"), 501); err == nil {
			t.Fatal("accepted invalid path")
		}
		if _, err := s.render(f.path(path), false); err == nil {
			t.Fatal("accepted invalid executable path")
		}
	}
	if _, err := NewLaunchService(s.Profile, "relative", 501); err == nil {
		t.Fatal("accepted relative home")
	}
	if _, err := NewLaunchService(s.Profile, f.path("home"), -1); err == nil {
		t.Fatal("accepted invalid uid")
	}
	must(t, os.Symlink(f.path("config.json"), f.path("link.json")))
	if _, err := NewLaunchService(f.path("link.json"), f.path("home"), 501); err == nil {
		t.Fatal("accepted linked profile")
	}
	must(t, s.Install(f.c, binary, false))
	lock := filepath.Join(s.metadataDir(), "sync.lock")
	must(t, os.WriteFile(lock, []byte("occupied"), 0600))
	run := func(context.Context, ...string) (int, error) { t.Fatal("called launchctl through lock"); return 0, nil }
	if s.Start(context.Background(), run) == nil || s.Stop(context.Background(), run) == nil || s.Uninstall(context.Background(), run) == nil {
		t.Fatal("ignored service lock")
	}
}

func TestServiceFailedStartPreservesInstallation(t *testing.T) {
	f, s, binary := serviceFixture(t)
	must(t, s.Install(f.c, binary, false))
	run := func(_ context.Context, args ...string) (int, error) {
		if args[0] == "print" {
			return 113, nil
		}
		return 5, nil
	}
	if s.Start(context.Background(), run) == nil {
		t.Fatal("accepted failed bootstrap")
	}
	_, stored, err := s.owned()
	must(t, err)
	if stored == nil {
		t.Fatal("lost installation")
	}
}
