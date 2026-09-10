package release

import (
	"context"
	"debug/buildinfo"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual compiler boundary: inheriting ambient build flags,
// dropping the version linker flag, or permitting module edits must be visible
// in the produced executable or in the unchanged fixture source tree.
func TestBuildRealIsolatedModule(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go compiler unavailable")
	}
	environment := t.TempDir()
	for _, key := range []string{"HOME", "GOCACHE", "GOPATH", "GOMODCACHE", "TMPDIR", "GOTMPDIR", "XDG_CONFIG_HOME", "TEST_TELEMETRY_DIR", "CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		path := filepath.Join(environment, strings.ToLower(key))
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, path)
	}
	t.Setenv("PATH", filepath.Dir(goTool)+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv("GOENV", "off")
	t.Setenv("GOCACHEPROG", "")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	target := Target{runtime.GOOS, runtime.GOARCH}

	t.Run("version and build environment", func(t *testing.T) {
		dir := releaseBuildFixture(t)
		t.Setenv("GOOS", "invalid-inherited-os")
		t.Setenv("GOARCH", "invalid-inherited-arch")
		t.Setenv("CGO_ENABLED", "1")
		t.Setenv("GOFLAGS", "-tags=ambient_poison")
		t.Setenv("GOWORK", filepath.Join(dir, "missing-workspace"))
		releaseFixtureWrite(t, "cmd/agent-bridge/cgo.go", "//go:build cgo\n\npackage main\nvar _ = cgoMustNotBeEnabled\n")
		releaseFixtureWrite(t, "cmd/agent-bridge/ambient.go", "//go:build ambient_poison\n\npackage main\nvar _ = ambientFlagsMustNotApply\n")
		output := filepath.Join(dir, "release-binary")
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := Build(ctx, target, "v7.8.9-rc.2", output); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, output)
		cmd.Env = []string{"HOME=" + filepath.Join(environment, "home"), "PATH=/usr/bin:/bin"}
		data, err := cmd.CombinedOutput()
		if err != nil || string(data) != "v7.8.9-rc.2\n" {
			t.Fatalf("release executable version: %v %q", err, data)
		}
		info, err := buildinfo.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		settings := map[string]string{}
		for _, setting := range info.Settings {
			settings[setting.Key] = setting.Value
		}
		for key, want := range map[string]string{"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "CGO_ENABLED": "0", "-trimpath": "true"} {
			if settings[key] != want {
				t.Fatalf("release build setting %s = %q, want %q", key, settings[key], want)
			}
		}
	})

	t.Run("unpinned local dependency cannot edit module", func(t *testing.T) {
		releaseBuildFixture(t)
		const module = "module github.com/Jhorlin/agent-bridge\n\ngo 1.23.0\n\nreplace example.test/unpinned => ./dependency\n"
		releaseFixtureWrite(t, "go.mod", module)
		releaseFixtureWrite(t, "dependency/go.mod", "module example.test/unpinned\n\ngo 1.23.0\n")
		releaseFixtureWrite(t, "dependency/dependency.go", "package dependency\n")
		releaseFixtureWrite(t, "cmd/agent-bridge/main.go", "package main\nimport _ \"example.test/unpinned\"\nfunc main() {}\n")
		if err := Build(context.Background(), target, "v1.2.3", "release-binary"); err == nil {
			t.Fatal("release build resolved an unpinned module")
		}
		data, err := os.ReadFile("go.mod")
		if err != nil || string(data) != module {
			t.Fatal("release build edited module requirements")
		}
		releaseAssertMissing(t, "go.sum")
		releaseAssertMissing(t, "release-binary")
	})

	t.Run("compiler diagnostics stay private", func(t *testing.T) {
		dir := releaseBuildFixture(t)
		releaseFixtureWrite(t, "cmd/agent-bridge/main.go", "package main\nfunc main() { PRIVATE_RELEASE_SOURCE_SENTINEL }\n")
		err := Build(context.Background(), target, "v1.2.3", "release-binary")
		if err == nil || !strings.Contains(err.Error(), "build "+target.OS+"/"+target.Arch+" failed") {
			t.Fatalf("compiler failure not reported: %v", err)
		}
		if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "PRIVATE_RELEASE_SOURCE_SENTINEL") {
			t.Fatal("compiler diagnostic leaked source contents or checkout path")
		}
		releaseAssertMissing(t, "release-binary")
	})

	t.Run("cancelled build publishes no executable", func(t *testing.T) {
		releaseBuildFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := Build(ctx, target, "v1.2.3", "release-binary"); !errors.Is(err, context.Canceled) {
			t.Fatalf("build cancellation not propagated: %v", err)
		}
		releaseAssertMissing(t, "release-binary")
	})

	t.Run("invalid version never starts compiler", func(t *testing.T) {
		releaseBuildFixture(t)
		t.Setenv("PATH", t.TempDir())
		err := Build(context.Background(), target, "v1.2.3 -X PRIVATE_VERSION_VALUE", "release-binary")
		if err == nil || errors.Is(err, exec.ErrNotFound) || !strings.Contains(err.Error(), "invalid build version") {
			t.Fatalf("version was not rejected before compiler lookup: %v", err)
		}
		if strings.Contains(err.Error(), "PRIVATE_VERSION_VALUE") {
			t.Fatal("invalid version leaked into error")
		}
		releaseAssertMissing(t, "release-binary")
	})
}

func TestPackageMissingInputsDoNotStartBuildOrCreateOutput(t *testing.T) {
	for _, missing := range []string{"LICENSE", "README.md"} {
		t.Run(missing, func(t *testing.T) {
			fixture(t)
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			called := false
			err := Package(context.Background(), "v1.2.3", "artifacts", func(context.Context, Target, string, string) error { called = true; return nil })
			if !errors.Is(err, os.ErrNotExist) || called {
				t.Fatalf("missing release input did not stop before build: %v", err)
			}
			releaseAssertMissing(t, "artifacts")
		})
	}
}

func TestPackageScratchFailureLeavesNoCompletionClaim(t *testing.T) {
	dir := fixture(t)
	t.Setenv("TMPDIR", filepath.Join(dir, "missing-scratch-parent"))
	called := false
	err := Package(context.Background(), "v1.2.3", "artifacts", func(context.Context, Target, string, string) error { called = true; return nil })
	if !errors.Is(err, os.ErrNotExist) || called {
		t.Fatalf("unavailable scratch directory did not stop before build: %v", err)
	}
	entries, err := os.ReadDir("artifacts")
	if err != nil || len(entries) != 0 {
		t.Fatal("scratch failure left misleading release artifacts")
	}
}

func TestPackageRejectsMissingAndNonregularBuilderProducts(t *testing.T) {
	for _, kind := range []string{"missing", "directory"} {
		t.Run(kind, func(t *testing.T) {
			fixture(t)
			calls := 0
			err := Package(context.Background(), "v1.2.3", "artifacts", func(_ context.Context, _ Target, _, output string) error {
				calls++
				if kind == "directory" {
					return os.Mkdir(output, 0700)
				}
				return nil
			})
			if err == nil || calls != 1 {
				t.Fatalf("invalid builder product accepted or later builds started: %v", err)
			}
			releaseAssertMissing(t, "artifacts/SHA256SUMS")
		})
	}
}

func TestPackageCancellationBetweenTargetsPreservesCompletedArchive(t *testing.T) {
	fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := Package(ctx, "v1.2.3", "artifacts", func(ctx context.Context, target Target, version, output string) error {
		calls++
		cancel()
		return fakeBuild(ctx, target, version, output)
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation started another target or was lost: %v", err)
	}
	files := readArchive(t, "artifacts/agent-bridge_v1.2.3_darwin_amd64.tar.gz")
	if string(files["agent-bridge"]) != "darwin/amd64 v1.2.3" || string(files["README.md"]) != "README.md fixture" {
		t.Fatal("completed archive evidence changed after cancellation")
	}
	releaseAssertMissing(t, "artifacts/SHA256SUMS")
	releaseAssertMissing(t, "artifacts/agent-bridge_v1.2.3_darwin_arm64.tar.gz")
}

func TestPackageArtifactCollisionDoesNotOverwriteAnotherFile(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		name := "file"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			dir := fixture(t)
			const protected = "original artifact from another publisher"
			const artifact = "artifacts/agent-bridge_v1.2.3_darwin_amd64.tar.gz"
			calls := 0
			err := Package(context.Background(), "v1.2.3", "artifacts", func(ctx context.Context, target Target, version, output string) error {
				calls++
				// Simulate another publisher occupying the name after Package's
				// output-directory check, before it publishes the first archive.
				if symlink {
					releaseFixtureWrite(t, "protected", protected)
					if err := os.Symlink(filepath.Join(dir, "protected"), artifact); err != nil {
						return err
					}
				} else {
					releaseFixtureWrite(t, artifact, protected)
				}
				return fakeBuild(ctx, target, version, output)
			})
			if !errors.Is(err, os.ErrExist) || calls != 1 {
				t.Fatalf("existing artifact was not protected: %v", err)
			}
			data, err := os.ReadFile(artifact)
			if err != nil || string(data) != protected {
				t.Fatal("existing artifact or symlink target overwritten")
			}
			releaseAssertMissing(t, "artifacts/SHA256SUMS")
		})
	}
}

func TestChecksumPublicationRequiresExistingParent(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "missing")
	if err := exclusiveWrite(filepath.Join(parent, "SHA256SUMS"), []byte("complete")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing publication directory was not reported: %v", err)
	}
	releaseAssertMissing(t, parent)
}

func releaseBuildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	releaseFixtureWrite(t, "go.mod", "module github.com/Jhorlin/agent-bridge\n\ngo 1.23.0\n")
	releaseFixtureWrite(t, "internal/cli/version.go", "package cli\nvar Version = \"unset\"\n")
	releaseFixtureWrite(t, "cmd/agent-bridge/main.go", "package main\nimport \"github.com/Jhorlin/agent-bridge/internal/cli\"\nfunc main() { println(cli.Version) }\n")
	return dir
}

func releaseFixtureWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func releaseAssertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s absent: %v", path, err)
	}
}
