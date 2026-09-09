package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"version": 1, "stateDir": "state", "resources": []map[string]any{{"id": "rules", "kind": "portable-file", "scope": "global", "claude": "CLAUDE.md", "codex": "AGENTS.md"}}})
	if err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(dir, "config.json")
}
func waitFile(t *testing.T, file, want string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(file)
		if string(b) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("watch did not synchronize %s", file)
}
func TestWatchApplyAndShutdown(t *testing.T) {
	dir, config := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", config, "--apply"}, io.Discard, io.Discard) }()
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("edited via codex"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(dir, "CLAUDE.md"), "edited via codex")
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop")
	}
}
func TestSyncConflictExitCode(t *testing.T) {
	dir, config := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"sync", config}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("conflict wrote state")
	}
}
func TestUsageAndUnknownFlags(t *testing.T) {
	for _, args := range [][]string{nil, {"nope", "config"}, {"sync", "config", "--apply"}, {"watch", "config", "--force"}, {"watch", "config", "--apply", "--apply"}} {
		var out bytes.Buffer
		if Run(context.Background(), args, io.Discard, &out) != 1 || out.Len() == 0 {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
func TestPlanAndRecoverCommands(t *testing.T) {
	_, config := setup(t)
	for _, cmd := range []string{"plan", "sync", "recover"} {
		var out bytes.Buffer
		if code := Run(context.Background(), []string{cmd, config}, &out, io.Discard); code != 0 {
			t.Fatalf("%s exit %d", cmd, code)
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("invalid JSON output for %s", cmd)
		}
	}
}
func TestWatchWithoutApplyIsReadOnly(t *testing.T) {
	dir, config := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()
	if code := Run(ctx, []string{"watch", config}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("read-only watcher wrote state")
	}
}

func TestMCPWatcherEndToEnd(t *testing.T) {
	dir, config := setup(t)
	raw := map[string]any{"version": 1, "stateDir": "state", "resources": []map[string]any{{"id": "tools", "kind": "mcp-config", "scope": "project", "claude": "mcp.json", "codex": "config.toml", "servers": []string{"docs"}, "allowReformat": true}}}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(config, b, 0600); err != nil {
		t.Fatal(err)
	}
	input := `{"mcpServers":{"docs":{"command":"original-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", config, "--apply"}, io.Discard, io.Discard) }()
	waitContains := func(file, want string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(file)
			if strings.Contains(string(data), want) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("MCP watcher did not update %s", file)
	}
	waitContains(filepath.Join(dir, "config.toml"), "original-server")
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "config.toml"), []byte(strings.Replace(string(data), "original-server", "codex-edited", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	waitContains(filepath.Join(dir, "mcp.json"), "codex-edited")
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop")
	}
}

func TestConfigCommandShowsResolvedPathsWithoutReadingNativeContents(t *testing.T) {
	dir, config := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("private native contents"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run(context.Background(), []string{"config", config}, &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "CLAUDE.md") || strings.Contains(out.String(), "private native contents") {
		t.Fatal("bad effective config output")
	}
}

func TestAuditFormatsExitCodesAndPrivacy(t *testing.T) {
	dir, config := setup(t)
	for _, flag := range []string{"", "--json"} {
		args := []string{"audit", config}
		if flag != "" {
			args = append(args, flag)
		}
		var out, errOut bytes.Buffer
		if code := Run(context.Background(), args, &out, &errOut); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		if flag != "" && !json.Valid(out.Bytes()) {
			t.Fatal("invalid audit JSON")
		}
		if !strings.Contains(out.String(), "review-required") {
			t.Fatal("missing review warning")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("audit created state")
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("PRIVATE_NATIVE_VALUE"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"audit", config, "--json"}, &out, &errOut); code != 2 {
		t.Fatalf("conflict exit %d", code)
	}
	if strings.Contains(out.String()+errOut.String(), "PRIVATE_NATIVE_VALUE") {
		t.Fatal("native value leaked")
	}
	if err := os.WriteFile(config, []byte(`{"version":1,"stateDir":"state","resources":[{"kind":"PRIVATE_KIND"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{"audit", config, "--json"}, &out, &errOut); code != 1 {
		t.Fatalf("invalid config exit %d", code)
	}
	if strings.Contains(out.String()+errOut.String(), "PRIVATE_KIND") {
		t.Fatal("profile value leaked")
	}
	for _, args := range [][]string{{"audit", config, "--apply"}, {"plan", config, "--json"}} {
		if Run(context.Background(), args, io.Discard, io.Discard) != 1 {
			t.Fatal("invalid flag accepted")
		}
	}
}

func TestAuditRejectsUnknownProfileFields(t *testing.T) {
	_, config := setup(t)
	data, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"PRIVATE_UNKNOWN":"secret","version":1`), 1)
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if Run(context.Background(), []string{"audit", config}, &out, &out) != 1 {
		t.Fatal("unknown option accepted")
	}
	if strings.Contains(out.String(), "PRIVATE_UNKNOWN") || strings.Contains(out.String(), "secret") {
		t.Fatal("unknown field leaked")
	}
}

type auditFailWriter struct{ remaining int }

func (w *auditFailWriter) Write(p []byte) (int, error) {
	if w.remaining == 0 {
		return 0, errors.New("PRIVATE_WRITER_ERROR")
	}
	w.remaining--
	return len(p), nil
}
func TestAuditOutputFailureIsRedacted(t *testing.T) {
	dir, config := setup(t)
	for _, format := range []string{"", "--json"} {
		for _, remaining := range []int{0, 1, 2} {
			if format != "" && remaining > 0 {
				continue
			}
			args := []string{"audit", config}
			if format != "" {
				args = append(args, format)
			}
			var errOut bytes.Buffer
			if code := Run(context.Background(), args, &auditFailWriter{remaining}, &errOut); code != 1 {
				t.Fatalf("output failure exit %d", code)
			}
			if strings.Contains(errOut.String(), "PRIVATE_WRITER_ERROR") {
				t.Fatal("writer error leaked")
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("failed output created state")
	}
}
