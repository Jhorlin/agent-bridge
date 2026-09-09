package bridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// LaunchService owns only its exact plist and receipt. Sync state is independent.
type LaunchService struct{ Profile, Root, Label, Domain string }
type serviceReceipt struct {
	Version int       `json:"version"`
	Profile string    `json:"profile"`
	Apply   bool      `json:"apply"`
	Plist   *Snapshot `json:"plist"`
}
type ServiceStatus struct {
	Label        string `json:"label"`
	Installed    bool   `json:"installed"`
	Registration string `json:"registration"`
	Apply        bool   `json:"apply"`
}
type LaunchRunner func(context.Context, ...string) (int, error)

func validServicePath(path string) bool {
	if !utf8.ValidString(path) {
		return false
	}
	for _, r := range path {
		if r < 32 || r == 0xfffe || r == 0xffff {
			return false
		}
	}
	return true
}

func NewLaunchService(profile, home string, uid int) (LaunchService, error) {
	s := LaunchService{}
	absolute, err := filepath.Abs(profile)
	if err != nil {
		return s, err
	}
	if !filepath.IsAbs(home) || uid < 0 {
		return s, fmt.Errorf("service requires an absolute home and user identity")
	}
	for _, path := range []string{absolute, home} {
		if !validServicePath(path) {
			return s, fmt.Errorf("invalid service path")
		}
		if err := assertSafe(path); err != nil {
			return s, err
		}
	}
	hash := sha256.Sum256([]byte(absolute))
	s = LaunchService{absolute, filepath.Join(home, "Library", "LaunchAgents"), "com.jhorlin.agent-bridge." + hex.EncodeToString(hash[:16]), fmt.Sprintf("gui/%d", uid)}
	return s, nil
}
func (s LaunchService) plistPath() string   { return filepath.Join(s.Root, s.Label+".plist") }
func (s LaunchService) metadataDir() string { return filepath.Join(s.Root, ".agent-bridge") }
func (s LaunchService) receiptPath() string { return filepath.Join(s.metadataDir(), s.Label+".json") }
func (s LaunchService) target() string      { return s.Domain + "/" + s.Label }

func (s LaunchService) prepareLogs() error {
	for _, suffix := range []string{".out.log", ".err.log"} {
		path := filepath.Join(s.metadataDir(), s.Label+suffix)
		if err := assertSafe(path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if err = createServiceFile(path, &Snapshot{Mode: 0600}); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err = checkRegular(info); err != nil {
			return err
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("service logs must be private")
		}
	}
	return nil
}

// Launchctl never inherits shell evaluation or prints native diagnostics/values.
func Launchctl(ctx context.Context, args ...string) (int, error) {
	if runtime.GOOS != "darwin" {
		return -1, fmt.Errorf("launchd services require macOS")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "/bin/launchctl", args...).Run()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), nil
	}
	return -1, fmt.Errorf("could not execute launchctl")
}

func (s LaunchService) render(binary string, apply bool) (*Snapshot, error) {
	if !filepath.IsAbs(binary) || !validServicePath(binary) {
		return nil, fmt.Errorf("service requires an absolute executable path")
	}
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict>\n")
	text := func(tag, value string) {
		b.WriteString("<" + tag + ">")
		xml.EscapeText(&b, []byte(value))
		b.WriteString("</" + tag + ">\n")
	}
	text("key", "Label")
	text("string", s.Label)
	text("key", "ProgramArguments")
	b.WriteString("<array>\n")
	for _, arg := range []string{binary, "watch", s.Profile} {
		text("string", arg)
	}
	if apply {
		text("string", "--apply")
	}
	b.WriteString("</array>\n")
	text("key", "WorkingDirectory")
	text("string", filepath.Dir(s.Profile))
	text("key", "RunAtLoad")
	b.WriteString("<true/>\n")
	// Transaction failures require inspection, not an automatic restart loop.
	text("key", "KeepAlive")
	b.WriteString("<false/>\n")
	text("key", "Umask")
	b.WriteString("<integer>63</integer>\n")
	text("key", "StandardOutPath")
	text("string", filepath.Join(s.metadataDir(), s.Label+".out.log"))
	text("key", "StandardErrorPath")
	text("string", filepath.Join(s.metadataDir(), s.Label+".err.log"))
	b.WriteString("</dict></plist>\n")
	return &Snapshot{Data: base64.StdEncoding.EncodeToString(b.Bytes()), Mode: 0600}, nil
}

func createServiceFile(path string, data *Snapshot) error {
	if err := assertSafe(path); err != nil {
		return err
	}
	raw, err := snapshotBytes(data)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// Install creates files for the next login, but does not register/start a job.
// Partial failures preserve evidence; existing files are never overwritten.
func (s LaunchService) Install(c Config, binary string, apply bool) error {
	if len(c.ConfigFiles) == 0 || c.ConfigFiles[0] != s.Profile {
		return fmt.Errorf("service profile does not match loaded config")
	}
	if err := assertSafe(binary); err != nil {
		return err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return err
	}
	if err = checkRegular(info); err != nil {
		return err
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("service binary is not executable")
	}
	for _, root := range append([]string{c.StateDir}, c.ConfigFiles...) {
		if serviceOverlap(root, s.Root) {
			return fmt.Errorf("service files overlap state or profile")
		}
	}
	if c.CoordinationDir != "" && serviceOverlap(c.CoordinationDir, s.Root) {
		return fmt.Errorf("service files overlap coordination directory")
	}
	for _, r := range c.Resources {
		for _, path := range r.Paths {
			if serviceOverlap(path, s.Root) {
				return fmt.Errorf("service files overlap a resource")
			}
		}
	}
	plist, err := s.render(binary, apply)
	if err != nil {
		return err
	}
	return directoryLocked(s.metadataDir(), func() error {
		for _, path := range []string{s.plistPath(), s.receiptPath()} {
			existing, err := snapshot(path)
			if err != nil {
				return err
			}
			if existing != nil {
				return fmt.Errorf("service files already exist; refusing overwrite")
			}
		}
		if err := s.prepareLogs(); err != nil {
			return err
		}
		if err := createServiceFile(s.plistPath(), plist); err != nil {
			return err
		}
		receipt, err := encoded(serviceReceipt{1, s.Profile, apply, plist})
		if err != nil {
			return err
		}
		return createServiceFile(s.receiptPath(), receipt)
	})
}
func serviceOverlap(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return inside(a, b) || inside(b, a)
}

func (s LaunchService) owned() (serviceReceipt, *Snapshot, error) {
	var receipt serviceReceipt
	stored, err := snapshot(s.receiptPath())
	if err != nil {
		return receipt, nil, err
	}
	plist, err := snapshot(s.plistPath())
	if err != nil {
		return receipt, nil, err
	}
	if stored == nil && plist == nil {
		return receipt, nil, nil
	}
	if err := decode(stored, &receipt); err != nil {
		return receipt, nil, fmt.Errorf("service receipt missing or invalid; preserve files for inspection")
	}
	if receipt.Version != 1 || receipt.Profile != s.Profile || receipt.Plist == nil || valid(receipt.Plist) != nil || !equal(plist, receipt.Plist) {
		return receipt, nil, fmt.Errorf("service files changed or ownership is invalid; refusing operation")
	}
	return receipt, stored, nil
}
func (s LaunchService) registration(ctx context.Context, run LaunchRunner) (bool, error) {
	code, err := run(ctx, "print", s.target())
	if err != nil {
		return false, err
	}
	if code == 0 {
		return true, nil
	}
	if code == 113 {
		return false, nil
	}
	return false, fmt.Errorf("launchd registration status unavailable")
}
func (s LaunchService) Status(ctx context.Context, run LaunchRunner) (ServiceStatus, error) {
	status := ServiceStatus{Label: s.Label, Registration: "not-installed"}
	receipt, stored, err := s.owned()
	if err != nil {
		return status, err
	}
	if stored == nil {
		return status, nil
	}
	status.Installed = true
	status.Apply = receipt.Apply
	status.Registration = "unknown"
	registered, err := s.registration(ctx, run)
	if err != nil {
		return status, err
	}
	status.Registration = "unregistered"
	if registered {
		status.Registration = "registered"
	}
	return status, nil
}
func (s LaunchService) Start(ctx context.Context, run LaunchRunner) error {
	_, stored, err := s.owned()
	if err != nil {
		return err
	}
	if stored == nil {
		return fmt.Errorf("service is not installed")
	}
	return directoryLocked(s.metadataDir(), func() error {
		_, stored, err := s.owned()
		if err != nil {
			return err
		}
		if stored == nil {
			return fmt.Errorf("service is not installed")
		}
		registered, err := s.registration(ctx, run)
		if err != nil {
			return err
		}
		if err := s.prepareLogs(); err != nil {
			return err
		}
		args := []string{"bootstrap", s.Domain, s.plistPath()}
		if registered {
			args = []string{"kickstart", s.target()}
		}
		code, err := run(ctx, args...)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("service start failed; files preserved for inspection")
		}
		return nil
	})
}
func (s LaunchService) stop(ctx context.Context, run LaunchRunner) error {
	registered, err := s.registration(ctx, run)
	if err != nil || !registered {
		return err
	}
	code, err := run(ctx, "bootout", "--wait", s.target())
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("service stop failed; files preserved")
	}
	registered, err = s.registration(ctx, run)
	if err != nil {
		return err
	}
	if registered {
		return fmt.Errorf("service remains registered; refusing removal")
	}
	return nil
}
func (s LaunchService) Stop(ctx context.Context, run LaunchRunner) error {
	_, stored, err := s.owned()
	if err != nil || stored == nil {
		return err
	}
	return directoryLocked(s.metadataDir(), func() error {
		_, stored, err := s.owned()
		if err != nil || stored == nil {
			return err
		}
		return s.stop(ctx, run)
	})
}
func (s LaunchService) Uninstall(ctx context.Context, run LaunchRunner) error {
	_, stored, err := s.owned()
	if err != nil || stored == nil {
		return err
	}
	return directoryLocked(s.metadataDir(), func() error {
		_, stored, err := s.owned()
		if err != nil || stored == nil {
			return err
		}
		if err = s.stop(ctx, run); err != nil {
			return err
		}
		_, after, err := s.owned()
		if err != nil {
			return err
		}
		if !equal(stored, after) {
			return fmt.Errorf("service receipt changed during removal")
		}
		if err = os.Remove(s.plistPath()); err != nil {
			return err
		}
		return os.Remove(s.receiptPath())
	})
}
