// Package release builds distribution archives without third-party tooling.
package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)

type Target struct{ OS, Arch string }
type Builder func(context.Context, Target, string, string) error

var targets = []Target{{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}}

// Build uses the caller's checked-out repository and pinned module versions.
func Build(ctx context.Context, target Target, version, output string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid build version")
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X github.com/Jhorlin/agent-bridge/internal/cli.Version="+version, "-o", output, "./cmd/agent-bridge")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" && key != "GOFLAGS" && key != "GOWORK" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS="+target.OS, "GOARCH="+target.Arch, "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
	// Native compiler output may contain local paths; retain only the outcome.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s/%s failed: %w", target.OS, target.Arch, err)
	}
	return nil
}

// Package requires a new output directory. On failure, partial artifacts remain
// for inspection, with no checksums file claiming completion. Existing output
// is never removed or overwritten. Run from the repository root.
func Package(ctx context.Context, version, output string, build Builder) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("version must be vMAJOR.MINOR.PATCH with an optional prerelease suffix")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return fmt.Errorf("output must be a new directory: %w", err)
	}
	tmp, err := os.MkdirTemp("", "agent-bridge-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var checksums strings.Builder
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		binary := filepath.Join(tmp, "agent-bridge-"+target.OS+"-"+target.Arch)
		if err := build(ctx, target, version, binary); err != nil {
			return err
		}
		name := fmt.Sprintf("agent-bridge_%s_%s_%s.tar.gz", version, target.OS, target.Arch)
		path := filepath.Join(output, name)
		if err := archive(path, binary, license, readme); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(&checksums, "%x  %s\n", h.Sum(nil), name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return exclusiveWrite(filepath.Join(output, "SHA256SUMS"), []byte(checksums.String()))
}

func exclusiveWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".checksums-")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Publish a complete file only, without replacing an existing checksum file.
	return os.Link(f.Name(), path)
}

func archive(path, binary string, license, readme []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	add := func(name string, mode, size int64, source io.Reader) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: size, ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := io.CopyN(tw, source, size)
		return err
	}
	b, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer b.Close()
	info, err := b.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("builder did not produce a nonempty regular binary")
	}
	if err := add("agent-bridge", 0755, info.Size(), b); err != nil {
		return err
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{{"LICENSE", license}, {"README.md", readme}} {
		if err := add(entry.name, 0644, int64(len(entry.data)), strings.NewReader(string(entry.data))); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
