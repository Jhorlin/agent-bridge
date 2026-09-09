// Package diagnostics persists only a closed, content-free event schema.
package diagnostics

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

const MaxBytes = 1 << 20
const Archives = 3

var ErrUnsafe = errors.New("unsafe diagnostic path or record")
var hexRef = regexp.MustCompile(`^[a-f0-9]{64}$`)
var runRef = regexp.MustCompile(`^[a-f0-9]{32}$`)
var transactionRef = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var versionRef = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-(?:alpha|beta|rc)\.[0-9]+)?$`)
var revisionRef = regexp.MustCompile(`^[a-f0-9]{40}$`)

// No error strings, command arguments, paths, resource names or payloads belong here.
type Event struct {
	Schema      int    `json:"schema"`
	Time        string `json:"time"`
	Level       string `json:"level"`
	Version     string `json:"version"`
	Revision    string `json:"revision,omitempty"`
	Dirty       bool   `json:"dirty,omitempty"`
	Run         string `json:"run"`
	Profile     string `json:"profileRef"`
	Command     string `json:"command"`
	Stage       string `json:"stage"`
	Code        string `json:"code"`
	Component   string `json:"component"`
	Resource    string `json:"resourceRef,omitempty"`
	Path        string `json:"pathRef,omitempty"`
	Transaction string `json:"transaction,omitempty"`
	DurationMS  int64  `json:"durationMs,omitempty"`
	Exit        int    `json:"exit,omitempty"`
}

type Observer func(Event)

func Ref(s string) string {
	if s == "" {
		return ""
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
func BuildVersion(s string) string {
	if s == "dev" || (len(s) <= 64 && versionRef.MatchString(s)) {
		return s
	}
	return "custom"
}

func Revision() (string, bool) {
	if info, ok := debug.ReadBuildInfo(); ok {
		revision, dirty := "", false
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && revisionRef.MatchString(s.Value) {
				revision = s.Value
			}
			if s.Key == "vcs.modified" && s.Value == "true" {
				dirty = true
			}
		}
		return revision, dirty
	}
	return "", false
}
func Code(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "storage_full"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "operation_failed"
	}
}
func Member(s, choices string) bool {
	return strings.Contains("|"+choices+"|", "|"+s+"|") && s != "" && !strings.Contains(s, "|")
}
func valid(e Event) bool {
	_, err := time.Parse(time.RFC3339Nano, e.Time)
	return err == nil && len(e.Time) <= 35 && e.Schema == 1 &&
		Member(e.Level, "info|warning|error") && len(e.Version) <= 64 && (e.Version == "dev" || e.Version == "custom" || versionRef.MatchString(e.Version)) &&
		(e.Revision == "" || revisionRef.MatchString(e.Revision)) &&
		runRef.MatchString(e.Run) && hexRef.MatchString(e.Profile) &&
		Member(e.Command, "sync|sync-reviewed|watch|recover|init|enroll-reviewed|create-enrolled|recover-enrollment|resolve-reviewed|restore-reviewed|apply-retirement|apply-file-change|recover-file-change|apply-file-change-undo|service-install|service-start|service-stop|service-uninstall") &&
		Member(e.Stage, "command_start|command_finish|config_load|plan|expand|read|normalize|lock|prepare|render|validate|journal|write|receipt|commit|rollback|recover|watch_paused|watch_resumed|conflict|operation|panic") &&
		Member(e.Code, "ok|operation_failed|storage_full|permission_denied|not_found|already_exists|canceled|timeout|conflict|blocked|observation_changed|pending_recovery|lock_present|panic") &&
		Member(e.Component, "cli|bridge.plan|bridge.transaction|bridge.recovery|cli.service|cli.enrollment|cli.file_change|cli.history|cli.resolve") &&
		(e.Resource == "" || hexRef.MatchString(e.Resource)) && (e.Path == "" || hexRef.MatchString(e.Path)) &&
		(e.Transaction == "" || transactionRef.MatchString(e.Transaction)) && e.DurationMS >= 0 && e.Exit >= 0 && e.Exit <= 255
}

// Directory is stable even when a profile is missing or fails validation.
func Directory(profile, home, stateHome, platform string) (string, error) {
	abs, err := filepath.Abs(profile)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		return "", ErrUnsafe
	}
	base := filepath.Join(home, ".local", "state")
	if platform == "darwin" {
		base = filepath.Join(home, "Library", "Logs")
	} else if stateHome != "" {
		if !filepath.IsAbs(stateHome) {
			return "", ErrUnsafe
		}
		base = stateHome
	}
	return filepath.Join(base, "agent-bridge", Ref(abs)), nil
}

// Safe checks every existing ancestor without following symbolic links.
func Safe(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && (fi.Mode()&os.ModeSymlink != 0 || (p != abs && !fi.IsDir())) {
			return ErrUnsafe
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func private(fi os.FileInfo, dir bool) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid()) && fi.IsDir() == dir && fi.Mode().Perm()&0077 == 0 &&
		(dir || (fi.Mode().IsRegular() && st.Nlink == 1))
}
func check(path string, dir bool) error {
	if err := Safe(path); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !private(fi, dir) {
		return ErrUnsafe
	}
	return nil
}
func openPrivate(path string, flags int) (*os.File, error) {
	if err := Safe(path); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(path); err == nil && !private(fi, false) {
		return nil, ErrUnsafe
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	after, e := os.Lstat(path)
	if err != nil || e != nil || !private(fi, false) || !os.SameFile(fi, after) {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}

type Logger struct {
	mu       sync.Mutex
	dir      string
	base     Event
	warn     io.Writer
	disabled bool
	limit    int64
	write    func(*os.File, []byte) (int, error)
	last     Event
	lastTime time.Time
}

func New(dir, profile, command, version string, warning io.Writer) (*Logger, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(profile)
	if err != nil {
		return nil, err
	}
	l := &Logger{dir: dir, warn: warning, limit: MaxBytes, write: func(f *os.File, b []byte) (int, error) { return f.Write(b) },
		base: Event{Schema: 1, Version: BuildVersion(version), Run: hex.EncodeToString(id[:]), Profile: Ref(abs), Command: command}}
	l.base.Revision, l.base.Dirty = Revision()
	return l, nil
}

// Emit is best-effort and never propagates a storage failure into synchronization.
// Identical consecutive events are rate limited to one per minute (watch retries).
func (l *Logger) Emit(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.disabled {
		return
	}
	if e == l.last && time.Since(l.lastTime) < time.Minute {
		return
	}
	l.last, l.lastTime = e, time.Now()
	e.Schema, e.Version, e.Run, e.Profile, e.Command = 1, l.base.Version, l.base.Run, l.base.Profile, l.base.Command
	e.Revision, e.Dirty = l.base.Revision, l.base.Dirty
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if e.Level == "" {
		e.Level = "info"
		if e.Code != "ok" {
			e.Level = "error"
		}
	}
	if !valid(e) {
		l.fail()
		return
	}
	b, err := json.Marshal(e)
	if err == nil {
		err = l.append(append(b, '\n'))
	}
	if err != nil {
		l.fail()
	}
}
func (l *Logger) fail() {
	l.disabled = true
	if l.warn != nil {
		fmt.Fprintln(l.warn, "Diagnostic logging unavailable (log_write_failed); operation continues. Use doctor to inspect logging.")
	}
}

func (l *Logger) Disable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.disabled {
		l.fail()
	}
}
func (l *Logger) append(b []byte) error {
	if int64(len(b)) > l.limit {
		return ErrUnsafe
	}
	if err := Safe(l.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0700); err != nil {
		return err
	}
	if err := check(l.dir, true); err != nil {
		return err
	}
	lock, err := openPrivate(filepath.Join(l.dir, "log.lock"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// Validate all destinations before moving or removing any archive.
	for i := 0; i <= Archives; i++ {
		if err := check(logPath(l.dir, i), false); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	current := logPath(l.dir, 0)
	if fi, err := os.Lstat(current); err == nil && fi.Size()+int64(len(b)) > l.limit {
		if err = os.Remove(logPath(l.dir, Archives)); err != nil && !os.IsNotExist(err) {
			return err
		}
		for i := Archives - 1; i >= 0; i-- {
			if err = os.Rename(logPath(l.dir, i), logPath(l.dir, i+1)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	f, err := openPrivate(current, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	n, err := l.write(f, b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
func logPath(dir string, n int) string {
	if n == 0 {
		return filepath.Join(dir, "events.jsonl")
	}
	return filepath.Join(dir, fmt.Sprintf("events.%d.jsonl", n))
}

type ReadResult struct {
	Events   []Event `json:"events"`
	Rejected int     `json:"rejectedRecords"`
	Status   string  `json:"status"`
}

// Read never creates files, follows links, reads snapshots or returns unvalidated text.
func Read(dir string, tail int) ReadResult {
	r := ReadResult{Events: []Event{}, Status: "ok"}
	if tail < 1 || tail > 1000 {
		r.Status = "invalid_limit"
		return r
	}
	if err := check(dir, true); err != nil {
		r.Status = "unavailable"
		if os.IsNotExist(err) {
			r.Status = "not_created"
		}
		return r
	}
	for i := Archives; i >= 0; i-- {
		f, err := openPrivate(logPath(dir, i), os.O_RDONLY)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			r.Status = "unavailable"
			continue
		}
		fi, err := f.Stat()
		if err != nil || fi.Size() > MaxBytes {
			f.Close()
			r.Status = "unavailable"
			continue
		}
		scanner := bufio.NewScanner(io.LimitReader(f, MaxBytes+1))
		scanner.Buffer(make([]byte, 4096), 8192)
		for scanner.Scan() {
			var e Event
			dec := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
			dec.DisallowUnknownFields()
			if dec.Decode(&e) != nil || dec.Decode(new(any)) != io.EOF || !valid(e) {
				r.Rejected++
				continue
			}
			r.Events = append(r.Events, e)
			if len(r.Events) > tail {
				r.Events = r.Events[1:]
			}
		}
		if scanner.Err() != nil {
			r.Rejected++
			r.Status = "incomplete"
		}
		f.Close()
	}
	return r
}

// WriteBundle publishes a complete, fsynced private file exclusively.
func WriteBundle(path string, data []byte) error {
	return writeBundle(path, data, func(f *os.File, b []byte) (int, error) { return f.Write(b) })
}

func writeBundle(path string, data []byte, write func(*os.File, []byte) (int, error)) (err error) {
	if err = Safe(path); err != nil {
		return err
	}
	if _, err = os.Lstat(path); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".agent-bridge-bundle-*")
	if err != nil {
		return err
	}
	name := f.Name()
	owned, statErr := f.Stat()
	if statErr != nil {
		f.Close()
		_ = os.Remove(name)
		return statErr
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, f.Close())
		}
		if current, e := os.Lstat(name); e == nil && os.SameFile(owned, current) {
			err = errors.Join(err, os.Remove(name))
		}
	}()
	n, err := write(f, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	err = f.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = Safe(path); err != nil {
		return err
	}
	if current, e := os.Lstat(name); e != nil || !os.SameFile(owned, current) || !private(current, false) {
		return ErrUnsafe
	}
	return os.Link(name, path)
}

func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
