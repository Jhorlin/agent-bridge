package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryWatchReportsNewCandidatesWithoutReadingContents(t *testing.T) {
	for _, scope := range []string{"--global", "--project"} {
		t.Run(scope, func(t *testing.T) {
			dir, _ := setup(t)
			messages := make(watchMessages, 8)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() { done <- Run(ctx, []string{"watch-discovery", dir, scope}, messages, io.Discard) }()
			t.Cleanup(func() {
				cancel()
				select {
				case code := <-done:
					if code != 0 {
						t.Errorf("watch exit %d", code)
					}
				case <-time.After(3 * time.Second):
					t.Error("watch did not stop")
				}
			})
			next := func() string {
				t.Helper()
				select {
				case data := <-messages:
					if !json.Valid([]byte(data)) || !strings.Contains(data, `"readOnly":true`) {
						t.Fatal("invalid discovery report")
					}
					return data
				case <-time.After(4 * time.Second):
					t.Fatal("missing discovery update")
					return ""
				}
			}
			next()
			// A real, unchanged upstream sidecar in a synthetic disposable skill tree.
			// Inventory must not read/execute its default_prompt or enroll the skill.
			data, err := os.ReadFile("../bridge/testdata/upstream/cli-creator/openai.yaml")
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(dir, ".agents", "skills", "upstream-demo")
			if err := os.MkdirAll(filepath.Join(root, "agents"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agents", "openai.yaml"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("PRIVATE fixture body: never read during discovery"), 0600); err != nil {
				t.Fatal(err)
			}
			result := next()
			if !strings.Contains(result, "skill-directory-upstream-demo") || strings.Contains(result, "PRIVATE") || strings.Contains(result, "Create a composable CLI") || strings.Contains(result, `"portable":true`) {
				t.Fatal("discovery leaked content or granted portability")
			}
			select {
			case <-messages:
				t.Fatal("unchanged candidate inventory emitted again")
			case <-time.After(1100 * time.Millisecond):
			}
			if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
				t.Fatal("created counterpart config")
			}
			if _, err := os.Stat(filepath.Join(dir, ".agent-bridge")); !os.IsNotExist(err) {
				t.Fatal("created sync state")
			}
		})
	}
}

func TestDiscoveryWatchRejectsUnsafeRootsAndWriteFlags(t *testing.T) {
	dir, _ := setup(t)
	var errors bytes.Buffer
	for _, args := range [][]string{{"watch-discovery", dir, "--apply"}, {"watch-discovery", filepath.Join(dir, "missing"), "--global"}} {
		if Run(context.Background(), args, io.Discard, &errors) != 1 {
			t.Fatal("accepted invalid discovery watch")
		}
	}
	if Run(context.Background(), []string{"watch-discovery", dir, "--global"}, &auditFailWriter{}, io.Discard) != 1 {
		t.Fatal("ignored output failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Run(ctx, []string{"watch-discovery", dir, "--global"}, io.Discard, io.Discard) != 0 {
		t.Fatal("cancellation failed")
	}
}
