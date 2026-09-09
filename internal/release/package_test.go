package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"LICENSE", "README.md"} {
		if err := os.WriteFile(name, []byte(name+" fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func fakeBuild(_ context.Context, target Target, version, output string) error {
	return os.WriteFile(output, []byte(target.OS+"/"+target.Arch+" "+version), 0700)
}
func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name != "agent-bridge" && h.Name != "LICENSE" && h.Name != "README.md" {
			t.Fatal("unexpected archive entry", h.Name)
		}
		if h.Typeflag != tar.TypeReg || !h.ModTime.Equal(time.Unix(0, 0)) {
			t.Fatal("unsafe or nondeterministic header")
		}
		if h.Name == "agent-bridge" && h.Mode != 0755 {
			t.Fatal("binary not executable")
		}
		if _, ok := files[h.Name]; ok {
			t.Fatal("duplicate archive entry")
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[h.Name] = data
	}
	if len(files) != 3 {
		t.Fatal("incomplete archive")
	}
	return files
}

func TestPackageArchivesChecksumsAndDeterminism(t *testing.T) {
	dir := fixture(t)
	for _, name := range []string{"one", "two"} {
		if err := Package(context.Background(), "v0.1.0-alpha.1", filepath.Join(dir, name), fakeBuild); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir("one")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatal("unexpected artifacts")
	}
	sums, err := os.ReadFile("one/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		a, err := os.ReadFile(filepath.Join("one", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join("two", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("non-reproducible archive", entry.Name())
		}
		if entry.Name() == "SHA256SUMS" {
			continue
		}
		if !strings.Contains(string(sums), fmt.Sprintf("%x  %s\n", sha256.Sum256(a), entry.Name())) {
			t.Fatal("checksum mismatch")
		}
		files := readArchive(t, filepath.Join("one", entry.Name()))
		if string(files["LICENSE"]) != "LICENSE fixture" {
			t.Fatal("license mismatch")
		}
	}
	if err := Package(context.Background(), "v0.1.0", "one", fakeBuild); err == nil {
		t.Fatal("overwrote output")
	}
}

func TestPackageFailureDoesNotClaimCompletion(t *testing.T) {
	fixture(t)
	for _, version := range []string{"", "latest", "v1.0", "v1.0.0 -X secret=x", "../../v1.0.0"} {
		if err := Package(context.Background(), version, "invalid", fakeBuild); err == nil {
			t.Fatal("accepted invalid version")
		}
	}
	if _, err := os.Stat("invalid"); !os.IsNotExist(err) {
		t.Fatal("invalid version created output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Package(ctx, "v0.1.0", "cancelled", fakeBuild); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	calls := 0
	err := Package(context.Background(), "v0.1.0", "partial", func(ctx context.Context, target Target, version, path string) error {
		calls++
		if calls == 2 {
			return errors.New("fixture build failure")
		}
		return fakeBuild(ctx, target, version, path)
	})
	if err == nil {
		t.Fatal("ignored build failure")
	}
	if _, err := os.Stat("partial/SHA256SUMS"); !os.IsNotExist(err) {
		t.Fatal("published incomplete checksums")
	}
	entries, err := os.ReadDir("partial")
	if err != nil || len(entries) != 1 {
		t.Fatal("did not preserve partial evidence")
	}
}

func TestNativePackage(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_PACKAGE_TESTS") != "1" {
		t.Skip("set AGENT_BRIDGE_PACKAGE_TESTS=1 for real cross-build/archive smoke test")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	dir := t.TempDir()
	output := filepath.Join(dir, "artifacts")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := Package(ctx, "v0.1.0-alpha.1", output, Build); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		files := readArchive(t, filepath.Join(output, fmt.Sprintf("agent-bridge_v0.1.0-alpha.1_%s_%s.tar.gz", target.OS, target.Arch)))
		if target.OS == "darwin" {
			binary, err := macho.NewFile(bytes.NewReader(files["agent-bridge"]))
			if err != nil {
				t.Fatal(err)
			}
			want := macho.CpuAmd64
			if target.Arch == "arm64" {
				want = macho.CpuArm64
			}
			if binary.Cpu != want {
				t.Fatal("wrong Mach-O architecture")
			}
			binary.Close()
		} else {
			binary, err := elf.NewFile(bytes.NewReader(files["agent-bridge"]))
			if err != nil {
				t.Fatal(err)
			}
			want := elf.EM_X86_64
			if target.Arch == "arm64" {
				want = elf.EM_AARCH64
			}
			if binary.Machine != want {
				t.Fatal("wrong ELF architecture")
			}
			binary.Close()
		}
		if target.OS == runtime.GOOS && target.Arch == runtime.GOARCH {
			binary := filepath.Join(dir, "agent-bridge")
			if err := os.WriteFile(binary, files["agent-bridge"], 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, binary, "--version")
			cmd.Env = []string{"HOME=" + filepath.Join(dir, "unused-home"), "PATH=/usr/bin:/bin"}
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != "agent-bridge v0.1.0-alpha.1\n" {
				t.Fatalf("version smoke: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(dir, "unused-home")); !os.IsNotExist(err) {
				t.Fatal("version command created user files")
			}
		}
	}
}

func TestChecksumPublicationDoesNotOverwrite(t *testing.T) {
	fixture(t)
	if err := exclusiveWrite("SHA256SUMS", []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := exclusiveWrite("SHA256SUMS", []byte("replacement")); err == nil {
		t.Fatal("overwrote checksums")
	}
	data, err := os.ReadFile("SHA256SUMS")
	if err != nil || string(data) != "original" {
		t.Fatal("original changed")
	}
	files, err := filepath.Glob(".checksums-*")
	if err != nil || len(files) != 0 {
		t.Fatal("temporary checksums left behind")
	}
}

func TestPackageRejectsEmptyBuilderOutputAndFinalCancellation(t *testing.T) {
	fixture(t)
	if err := Package(context.Background(), "v0.1.0", "empty", func(_ context.Context, _ Target, _, path string) error { return os.WriteFile(path, nil, 0600) }); err == nil {
		t.Fatal("accepted empty binary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := Package(ctx, "v0.1.0", "cancel-final", func(ctx context.Context, target Target, version, path string) error {
		calls++
		if calls == 4 {
			cancel()
		}
		return fakeBuild(ctx, target, version, path)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat("cancel-final/SHA256SUMS"); !os.IsNotExist(err) {
		t.Fatal("claimed completion after cancellation")
	}
}
