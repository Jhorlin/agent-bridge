package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func root(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func logger(t *testing.T) (*Logger, *bytes.Buffer) {
	t.Helper()
	dir := filepath.Join(root(t), "logs")
	var warning bytes.Buffer
	l, e := New(dir, "/private/CANARY_PROFILE", "sync", "dev", &warning)
	if e != nil {
		t.Fatal(e)
	}
	return l, &warning
}
func writeEvent(i int) Event {
	return Event{Stage: "write", Component: "bridge.transaction", Code: "ok", Resource: Ref("CANARY_ID"), Path: Ref(fmt.Sprint("CANARY_PATH", i))}
}

func TestPrivateStructuredLogAndRunIdentity(t *testing.T) {
	l, w := logger(t)
	l.Emit(writeEvent(0))
	r := Read(l.dir, 50)
	if r.Status != "ok" || len(r.Events) != 1 || r.Rejected != 0 || w.Len() != 0 {
		t.Fatalf("%+v %s", r, w)
	}
	data, e := os.ReadFile(logPath(l.dir, 0))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(data, []byte("CANARY")) {
		t.Fatal("secret leaked")
	}
	for _, p := range []string{l.dir, logPath(l.dir, 0), filepath.Join(l.dir, "log.lock")} {
		fi, e := os.Stat(p)
		if e != nil || fi.Mode().Perm()&0077 != 0 {
			t.Fatalf("nonprivate %s", p)
		}
	}
	second, e := New(l.dir, "/private/CANARY_PROFILE", "recover", "dev", w)
	if e != nil {
		t.Fatal(e)
	}
	second.Emit(Event{Stage: "recover", Component: "bridge.recovery", Code: "ok"})
	r = Read(l.dir, 50)
	if len(r.Events) != 2 || r.Events[0].Run == r.Events[1].Run || r.Events[0].Profile != r.Events[1].Profile {
		t.Fatal("run correlation broken")
	}
}
func TestRotationRetentionAndTail(t *testing.T) {
	l, w := logger(t)
	l.limit = 1200
	for i := 0; i < 40; i++ {
		l.Emit(writeEvent(i))
	}
	entries, e := os.ReadDir(l.dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != Archives+2 {
		t.Fatalf("unbounded files %d", len(entries))
	}
	for _, entry := range entries {
		fi, e := entry.Info()
		if e != nil || fi.Size() > l.limit {
			t.Fatal("rotation size exceeded")
		}
	}
	r := Read(l.dir, 2)
	if r.Rejected != 0 || len(r.Events) != 2 || r.Events[1].Path != Ref("CANARY_PATH39") || w.Len() != 0 {
		t.Fatalf("%+v %s", r, w)
	}
}
func TestStorageFailuresWarnOnceAndNeverPanic(t *testing.T) {
	for _, mode := range []string{"full", "short", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			l, w := logger(t)
			if mode == "full" {
				l.write = func(*os.File, []byte) (int, error) {
					return 0, &os.PathError{Op: "CANARY_OP", Path: "CANARY_SECRET", Err: syscall.ENOSPC}
				}
			}
			if mode == "short" {
				l.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:5]) }
			}
			e := writeEvent(0)
			if mode == "invalid" {
				e.Code = "CANARY_ERROR"
			}
			l.Emit(e)
			l.Emit(writeEvent(1))
			l.Disable()
			if strings.Count(w.String(), "log_write_failed") != 1 || strings.Contains(w.String(), "CANARY") || !l.disabled {
				t.Fatalf("unsafe warning %s", w)
			}
		})
	}
}
func TestUnsafeLogPathsFailClosed(t *testing.T) {
	for _, mode := range []string{"symlink", "hardlink", "public", "archive", "directory", "ancestor"} {
		t.Run(mode, func(t *testing.T) {
			l, w := logger(t)
			l.Emit(writeEvent(0))
			other := filepath.Join(root(t), "sentinel")
			if e := os.WriteFile(other, []byte("do not change"), 0600); e != nil {
				t.Fatal(e)
			}
			target := logPath(l.dir, 0)
			switch mode {
			case "symlink":
				if e := os.Remove(target); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(other, target); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Remove(target); e != nil {
					t.Fatal(e)
				}
				if e := os.Link(other, target); e != nil {
					t.Fatal(e)
				}
			case "public":
				if e := os.Chmod(target, 0644); e != nil {
					t.Fatal(e)
				}
			case "archive":
				if e := os.Symlink(other, logPath(l.dir, 3)); e != nil {
					t.Fatal(e)
				}
			case "directory":
				if e := os.Chmod(l.dir, 0755); e != nil {
					t.Fatal(e)
				}
			case "ancestor":
				moved := l.dir + "-real"
				if e := os.Rename(l.dir, moved); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(moved, l.dir); e != nil {
					t.Fatal(e)
				}
			}
			l.Emit(writeEvent(1))
			if !l.disabled || w.Len() == 0 {
				t.Fatal("unsafe target accepted")
			}
			b, e := os.ReadFile(other)
			if e != nil || string(b) != "do not change" {
				t.Fatal("outside target modified")
			}
			if mode != "archive" && Read(l.dir, 10).Status == "ok" {
				t.Fatal("unsafe log read")
			}
		})
	}
}
func TestReadRejectsForgedAndPartialRecords(t *testing.T) {
	l, _ := logger(t)
	l.Emit(writeEvent(0))
	data, e := os.ReadFile(logPath(l.dir, 0))
	if e != nil {
		t.Fatal(e)
	}
	bad := bytes.Replace(data, []byte(`"code":"ok"`), []byte(`"code":"CANARY_TOKEN"`), 1)
	extra := bytes.Replace(data, []byte(`"schema":1`), []byte(`"schema":1,"token":"CANARY_TOKEN"`), 1)
	combined := append(append(append([]byte{}, data...), bad...), extra...)
	combined = append(combined, []byte(`{"secret":"CANARY_TORN`)...)
	if e := os.WriteFile(logPath(l.dir, 0), combined, 0600); e != nil {
		t.Fatal(e)
	}
	r := Read(l.dir, 50)
	b, _ := json.Marshal(r)
	if r.Rejected != 3 || len(r.Events) != 1 || bytes.Contains(b, []byte("CANARY")) {
		t.Fatalf("unsafe read: %s", b)
	}
	if e := os.WriteFile(logPath(l.dir, 0), []byte(strings.Repeat("x", MaxBytes+1)), 0600); e != nil {
		t.Fatal(e)
	}
	if Read(l.dir, 50).Status != "unavailable" {
		t.Fatal("oversized log accepted")
	}
}
func TestConcurrentEventsAndLockContention(t *testing.T) {
	l, w := logger(t)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); l.Emit(writeEvent(i)) }(i)
	}
	wg.Wait()
	r := Read(l.dir, 100)
	if len(r.Events) != 50 || r.Rejected != 0 || w.Len() != 0 {
		t.Fatal("interleaved events")
	}
	f, e := openPrivate(filepath.Join(l.dir, "log.lock"), os.O_RDWR)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	second, e := New(l.dir, "/private/CANARY_PROFILE", "sync", "dev", w)
	if e != nil {
		t.Fatal(e)
	}
	second.Emit(writeEvent(99))
	if !second.disabled {
		t.Fatal("logger ignored cross-process lock")
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if len(Read(l.dir, 100).Events) != 50 {
		t.Fatal("contention corrupted records")
	}
}
func TestRepeatedFailuresAreRateLimited(t *testing.T) {
	l, _ := logger(t)
	e := Event{Stage: "plan", Component: "bridge.plan", Code: "operation_failed"}
	for i := 0; i < 100; i++ {
		l.Emit(e)
	}
	if len(Read(l.dir, 100).Events) != 1 {
		t.Fatal("retry flood")
	}
	l.Emit(Event{Stage: "watch_resumed", Component: "cli", Code: "ok"})
	l.Emit(e)
	if len(Read(l.dir, 100).Events) != 3 {
		t.Fatal("transition lost")
	}
}
func TestExclusiveBundleAndLocation(t *testing.T) {
	dir := root(t)
	path := filepath.Join(dir, "bundle.json")
	if e := WriteBundle(path, []byte("{}\n")); e != nil {
		t.Fatal(e)
	}
	if e := WriteBundle(path, []byte("overwrite")); !os.IsExist(e) {
		t.Fatal("existing bundle overwritten")
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("bundle not private")
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if WriteBundle(link, []byte("overwrite")) == nil {
		t.Fatal("bundle followed symlink")
	}
	for _, platform := range []string{"linux", "darwin"} {
		p, e := Directory("/profile.json", dir, "", platform)
		if e != nil || !strings.HasPrefix(p, dir) || !strings.HasSuffix(p, Ref("/profile.json")) {
			t.Fatal(p, e)
		}
	}
	if _, e := Directory("/profile.json", dir, "relative", "linux"); e == nil {
		t.Fatal("relative XDG accepted")
	}
	missing := filepath.Join(dir, "missing")
	if Read(missing, 20).Status != "not_created" {
		t.Fatal("missing logs status")
	}
	if _, e := os.Stat(missing); !os.IsNotExist(e) {
		t.Fatal("read created files")
	}
}
func TestErrorClassificationNeverUsesErrorText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{nil, "ok"}, {syscall.ENOSPC, "storage_full"}, {os.ErrPermission, "permission_denied"}, {os.ErrNotExist, "not_found"}, {os.ErrExist, "already_exists"}, {context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"}, {io.ErrShortWrite, "operation_failed"}, {errors.New("CANARY_TOKEN"), "operation_failed"}} {
		if Code(tc.err) != tc.code {
			t.Fatal(tc.code)
		}
	}
	if BuildVersion("CANARY_TOKEN") != "custom" || BuildVersion("v0.1.0-alpha.1") != "v0.1.0-alpha.1" {
		t.Fatal("unsafe build version")
	}
}

func TestBundleStorageFailureDoesNotPublishPartialFile(t *testing.T) {
	for _, mode := range []string{"full", "short", "racing-creator"} {
		t.Run(mode, func(t *testing.T) {
			dir := root(t)
			path := filepath.Join(dir, "bundle.json")
			err := writeBundle(path, []byte("complete bundle"), func(f *os.File, b []byte) (int, error) {
				if _, e := os.Lstat(path); !os.IsNotExist(e) {
					t.Fatal("partial final file visible")
				}
				if mode == "full" {
					return 0, syscall.ENOSPC
				}
				if mode == "short" {
					return f.Write(b[:3])
				}
				if e := os.WriteFile(path, []byte("external creator"), 0600); e != nil {
					t.Fatal(e)
				}
				return f.Write(b)
			})
			if err == nil {
				t.Fatal("injected failure ignored")
			}
			entries, e := os.ReadDir(dir)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "racing-creator" {
				b, e := os.ReadFile(path)
				if e != nil || string(b) != "external creator" || len(entries) != 1 {
					t.Fatal("external output changed")
				}
			} else if len(entries) != 0 {
				t.Fatal("partial bundle left behind")
			}
		})
	}
}
