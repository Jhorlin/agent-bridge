package bridge

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeSystemd struct {
	s        SystemdService
	state    string
	fail     string
	override bool
	calls    []string
}

func (f *fakeSystemd) run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if args[0] == f.fail {
		return "", errors.New("injected manager failure")
	}
	switch args[0] {
	case "show":
		drop := ""
		if f.override {
			drop = "/unreviewed/override.conf"
		}
		return "FragmentPath=" + f.s.unitPath() + "\nDropInPaths=" + drop + "\nActiveState=" + f.state, nil
	case "daemon-reload":
		return "", nil
	case "start":
		f.state = "active"
		return "", nil
	case "stop":
		f.state = "inactive"
		return "", nil
	}
	return "", errors.New("unexpected operation")
}

func systemdFixture(t *testing.T) (*fixture, SystemdService, string) {
	f := newFixture(t)
	s, err := NewSystemdService(f.path("config.json"), f.path("config-home"))
	must(t, err)
	f.write("bridge-binary", "inert executable fixture")
	must(t, os.Chmod(f.path("bridge-binary"), 0700))
	return f, s, f.path("bridge-binary")
}

func TestSystemdLifecycle(t *testing.T) {
	for _, apply := range []bool{false, true} {
		f, s, binary := systemdFixture(t)
		manager := &fakeSystemd{s: s, state: "inactive"}
		ctx := context.Background()
		status, err := s.Status(ctx, manager.run)
		must(t, err)
		if status.Installed {
			t.Fatal(status)
		}
		must(t, s.Install(f.c, binary, apply))
		if len(manager.calls) != 0 {
			t.Fatal("install called manager")
		}
		if err := s.Install(f.c, binary, apply); err == nil {
			t.Fatal("overwrote installed unit")
		}
		must(t, s.Start(ctx, manager.run))
		status, err = s.Status(ctx, manager.run)
		must(t, err)
		if !status.Installed || status.Registration != "active" || status.Apply != apply {
			t.Fatal(status)
		}
		must(t, s.Stop(ctx, manager.run))
		must(t, s.Start(ctx, manager.run))
		must(t, s.Uninstall(ctx, manager.run))
		must(t, s.Uninstall(ctx, manager.run))
		for _, path := range []string{s.unitPath(), s.receiptPath(), s.enablePath()} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("retained owned service file")
			}
		}
		f.expect("CLAUDE.md", "one")
		f.missing("state")
		f.missing("AGENTS.md")
	}
}

func TestSystemdFailuresPreserveOwnedFiles(t *testing.T) {
	for _, failure := range []string{"show", "stop", "override", "unit-edit", "link-edit", "lock"} {
		f, s, binary := systemdFixture(t)
		must(t, s.Install(f.c, binary, true))
		manager := &fakeSystemd{s: s, state: "active", fail: failure}
		switch failure {
		case "override":
			manager.override = true
		case "unit-edit":
			must(t, os.WriteFile(s.unitPath(), []byte("user edit"), 0600))
		case "link-edit":
			must(t, os.Remove(s.enablePath()))
			must(t, os.Symlink("/unreviewed", s.enablePath()))
		case "lock":
			must(t, os.WriteFile(s.metaDir()+"/sync.lock", []byte("stale lock"), 0600))
		}
		if err := s.Uninstall(context.Background(), manager.run); err == nil {
			t.Fatalf("accepted %s", failure)
		}
		if _, err := os.Stat(s.unitPath()); err != nil {
			t.Fatal("removed unit on failure")
		}
		if _, err := os.Stat(s.receiptPath()); err != nil {
			t.Fatal("removed receipt on failure")
		}
		f.expect("CLAUDE.md", "one")
	}
}

func TestSystemdRejectsOverlappingAndUnsafePaths(t *testing.T) {
	f, s, binary := systemdFixture(t)
	c := f.c
	c.StateDir = s.Root
	if err := s.Install(c, binary, false); err == nil {
		t.Fatal("overlap accepted")
	}
	for _, home := range []string{"relative", f.path("newline\npath")} {
		if _, err := NewSystemdService(f.path("config.json"), home); err == nil {
			t.Fatal("unsafe root accepted")
		}
	}
	if err := s.Start(context.Background(), (&fakeSystemd{s: s}).run); err == nil {
		t.Fatal("started missing unit")
	}
	must(t, s.Install(f.c, binary, false))
	bad := func(context.Context, ...string) (string, error) {
		return "FragmentPath=" + s.unitPath() + "\nActiveState=inactive", nil
	}
	if _, err := s.Status(context.Background(), bad); err == nil {
		t.Fatal("incomplete manager response accepted")
	}
}
