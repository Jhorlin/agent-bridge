package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type SystemdService struct{ Profile, Root, Label string }
type SystemdRunner func(context.Context, ...string) (string, error)
type systemdReceipt struct {
	Version int       `json:"version"`
	Profile string    `json:"profile"`
	Apply   bool      `json:"apply"`
	Unit    *Snapshot `json:"unit"`
}

// Systemctl uses only the calling user's manager. Diagnostics are never echoed.
func Systemctl(ctx context.Context, args ...string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("systemd requires Linux")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/systemctl", append([]string{"--user", "--no-pager"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("user service manager operation failed or timed out")
	}
	return strings.TrimSpace(string(out)), nil
}

func NewSystemdService(profile, configHome string) (SystemdService, error) {
	abs, err := filepath.Abs(profile)
	if err != nil {
		return SystemdService{}, err
	}
	for _, p := range []string{abs, configHome} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || !validServicePath(p) {
			return SystemdService{}, fmt.Errorf("invalid systemd path")
		}
		if err := assertSafe(p); err != nil {
			return SystemdService{}, err
		}
	}
	hash := sha256.Sum256([]byte(abs))
	return SystemdService{abs, filepath.Join(configHome, "systemd", "user"), fmt.Sprintf("agent-bridge-%x.service", hash[:16])}, nil
}

func (s SystemdService) unitPath() string    { return filepath.Join(s.Root, s.Label) }
func (s SystemdService) metaDir() string     { return filepath.Join(s.Root, ".agent-bridge") }
func (s SystemdService) receiptPath() string { return filepath.Join(s.metaDir(), s.Label+".json") }
func (s SystemdService) enablePath() string {
	return filepath.Join(s.Root, "default.target.wants", s.Label)
}

func (s SystemdService) owned() (systemdReceipt, bool, error) {
	var receipt systemdReceipt
	stored, err := snapshot(s.receiptPath())
	if err != nil {
		return receipt, false, err
	}
	unit, err := snapshot(s.unitPath())
	if err != nil {
		return receipt, false, err
	}
	if err := assertSafe(filepath.Dir(s.enablePath())); err != nil {
		return receipt, false, err
	}
	_, linkErr := os.Lstat(s.enablePath())
	if stored == nil && unit == nil && os.IsNotExist(linkErr) {
		return receipt, false, nil
	}
	if err := decode(stored, &receipt); err != nil {
		return receipt, false, fmt.Errorf("missing or invalid systemd receipt")
	}
	if receipt.Version != 1 || receipt.Profile != s.Profile || receipt.Unit == nil || valid(receipt.Unit) != nil || !equal(unit, receipt.Unit) {
		return receipt, false, fmt.Errorf("systemd unit changed; refusing operation")
	}
	target, err := os.Readlink(s.enablePath())
	if err != nil || target != s.unitPath() {
		return receipt, false, fmt.Errorf("systemd enable link changed; refusing operation")
	}
	return receipt, true, nil
}

// Install enables the owned unit at the next user-manager login. It never starts
// a service or changes a manager environment. Partial failures retain evidence.
func (s SystemdService) Install(c Config, binary string, apply bool) error {
	if len(c.ConfigFiles) == 0 || c.ConfigFiles[0] != s.Profile {
		return fmt.Errorf("profile mismatch")
	}
	paths := append([]string{c.StateDir, c.CoordinationDir}, c.ConfigFiles...)
	for _, r := range c.Resources {
		for _, p := range r.Paths {
			paths = append(paths, p)
		}
		for _, link := range r.Links {
			paths = append(paths, link.Path)
		}
	}
	for _, p := range paths {
		if p != "" && serviceOverlap(p, s.Root) {
			return fmt.Errorf("service directory overlaps managed files")
		}
	}
	unit, err := SystemdUnit(s.Profile, binary, apply)
	if err != nil {
		return err
	}
	return directoryLocked(s.metaDir(), func() error {
		for _, p := range []string{s.unitPath(), s.receiptPath(), s.enablePath()} {
			if err := assertSafe(p); err != nil {
				return err
			}
			if _, err := os.Lstat(p); !os.IsNotExist(err) {
				return fmt.Errorf("service files exist or are inaccessible; refusing overwrite")
			}
		}
		data := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(unit)), Mode: 0600}
		if err := createServiceFile(s.unitPath(), data); err != nil {
			return err
		}
		raw, err := encoded(systemdReceipt{1, s.Profile, apply, data})
		if err != nil {
			return err
		}
		if err := createServiceFile(s.receiptPath(), raw); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(s.enablePath()), 0700); err != nil {
			return err
		}
		return os.Symlink(s.unitPath(), s.enablePath())
	})
}

func (s SystemdService) managerState(ctx context.Context, run SystemdRunner) (string, error) {
	// Inspect the loaded fragment and drop-ins, not just a possibly ambiguous
	// is-active exit status. A replacement/override must not be started or removed.
	out, err := run(ctx, "show", s.Label, "--property=FragmentPath,DropInPaths,ActiveState")
	if err != nil {
		return "", err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", fmt.Errorf("invalid manager response")
		}
		if _, duplicate := fields[key]; duplicate {
			return "", fmt.Errorf("duplicate manager property")
		}
		fields[key] = value
	}
	if len(fields) != 3 {
		return "", fmt.Errorf("incomplete manager response")
	}
	for _, key := range []string{"FragmentPath", "DropInPaths", "ActiveState"} {
		if _, ok := fields[key]; !ok {
			return "", fmt.Errorf("missing manager property")
		}
	}
	if fields["FragmentPath"] != s.unitPath() || fields["DropInPaths"] != "" {
		return "", fmt.Errorf("manager unit is overridden or unavailable")
	}
	switch fields["ActiveState"] {
	case "active", "inactive", "failed", "activating", "deactivating", "reloading":
		return fields["ActiveState"], nil
	}
	return "", fmt.Errorf("unknown service state")
}

func (s SystemdService) Status(ctx context.Context, run SystemdRunner) (ServiceStatus, error) {
	status := ServiceStatus{Label: s.Label, Registration: "not-installed"}
	r, ok, err := s.owned()
	if err != nil || !ok {
		return status, err
	}
	status.Installed = true
	status.Apply = r.Apply
	status.Registration = "unknown"
	state, err := s.managerState(ctx, run)
	if err != nil {
		return status, err
	}
	status.Registration = state
	return status, nil
}

func (s SystemdService) mutate(ctx context.Context, run SystemdRunner, action string) error {
	_, ok, err := s.owned()
	if err != nil {
		return err
	}
	if !ok {
		if action == "start" {
			return fmt.Errorf("service not installed")
		}
		return nil
	}
	return directoryLocked(s.metaDir(), func() error {
		before, ok, err := s.owned()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("service disappeared")
		}
		if _, err := run(ctx, "daemon-reload"); err != nil {
			return err
		}
		if _, err := s.managerState(ctx, run); err != nil {
			return err
		}
		if action == "start" {
			if _, err := run(ctx, "start", s.Label); err != nil {
				return err
			}
			state, err := s.managerState(ctx, run)
			if err != nil {
				return err
			}
			if state != "active" {
				return fmt.Errorf("service did not become active")
			}
			return nil
		}
		if _, err := run(ctx, "stop", s.Label); err != nil {
			return err
		}
		state, err := s.managerState(ctx, run)
		if err != nil {
			return err
		}
		if state != "inactive" {
			return fmt.Errorf("service is not stopped; files preserved")
		}
		if action == "stop" {
			return nil
		}
		after, ok, err := s.owned()
		if err != nil {
			return err
		}
		if !ok || !equal(before.Unit, after.Unit) || before.Apply != after.Apply {
			return fmt.Errorf("service changed during removal")
		}
		if err := os.Remove(s.enablePath()); err != nil {
			return err
		}
		if err := os.Remove(s.unitPath()); err != nil {
			return err
		}
		if err := os.Remove(s.receiptPath()); err != nil {
			return err
		}
		_, err = run(ctx, "daemon-reload")
		return err
	})
}
func (s SystemdService) Start(ctx context.Context, run SystemdRunner) error {
	return s.mutate(ctx, run, "start")
}
func (s SystemdService) Stop(ctx context.Context, run SystemdRunner) error {
	return s.mutate(ctx, run, "stop")
}
func (s SystemdService) Uninstall(ctx context.Context, run SystemdRunner) error {
	return s.mutate(ctx, run, "uninstall")
}
