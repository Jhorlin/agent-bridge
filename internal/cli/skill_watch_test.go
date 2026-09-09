package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStrictSkillWatcherReverseAndReject(t *testing.T) {
	dir, config := setup(t)
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(config, `{"version":1,"stateDir":"state","resources":[{"id":"demo","kind":"skill-directory","scope":"global","portable":true,"allowReformat":true,"claude":"claude-skill","codex":"codex-skill"}]}`)
	claude, codex := filepath.Join(dir, "claude-skill/SKILL.md"), filepath.Join(dir, "codex-skill/SKILL.md")
	original := "---\nname: demo\ndescription: Initial description.\n---\nFixture instructions.\n"
	updated := "---\nname: demo\ndescription: Updated description.\n---\nFixture instructions.\n"
	write(claude, original)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", config, "--apply"}, io.Discard, io.Discard) }()
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
	waitFile(t, codex, original)
	write(codex, "---\nname: demo\ndescription: Unsafe metadata.\nallowed-tools: Bash\n---\nFixture instructions.\n")
	time.Sleep(2200 * time.Millisecond)
	data, err := os.ReadFile(claude)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatal("watch propagated unsupported metadata")
	}
	write(codex, updated)
	waitFile(t, claude, updated)
}
