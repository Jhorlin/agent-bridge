package hookguard

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPatchTargets(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: new.txt\n+text\n*** Update File: old.txt\n*** Move to: moved.txt\n@@\n-old\n+new\n*** Delete File: deleted.txt\n*** End Patch"
	paths, err := PatchTargets(patch)
	if err != nil || strings.Join(paths, ",") != "new.txt,old.txt,moved.txt,deleted.txt" {
		t.Fatal(paths, err)
	}
	for _, bad := range []string{"", "*** Begin Patch\n*** Environment ID: remote\n*** End Patch", "*** Begin Patch\n*** Update File: a\n*** Surprise: b\n*** End Patch", "*** Begin Patch\n*** Delete File: a\n+unparsed\n*** End Patch", "*** Begin Patch\n*** Add File: a\n*** End Patch", "*** Begin Patch\n*** Add File: a\n+x\n*** End Patch\n*** Delete File: b"} {
		if _, err := PatchTargets(bad); err == nil {
			t.Fatal("ambiguous patch accepted")
		}
	}
}

func FuzzPatchTargets(f *testing.F) {
	f.Add("*** Begin Patch\n*** Add File: x\n+x\n*** End Patch")
	f.Add("*** Begin Patch\n*** Update File: x\n*** Move to: y\n@@\n-x\n+y\n*** End Patch")
	f.Fuzz(func(t *testing.T, input string) {
		paths, err := PatchTargets(input)
		if err == nil {
			if len(paths) == 0 || len(paths) > 256 {
				t.Fatal("unbounded targets")
			}
			for _, path := range paths {
				if path == "" || strings.ContainsAny(path, "\x00\r\n\t") {
					t.Fatal("unsafe target")
				}
			}
		}
	})
}

func TestGuardBoundariesAndTimeout(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "guard")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n/bin/sleep 20\n"), 0700); err != nil {
		t.Fatal(err)
	}
	input := Input{Event: "PreToolUse", Tool: "apply_patch", Cwd: dir, ToolInput: map[string]any{"command": "*** Begin Patch\n*** Add File: x\n+x\n*** End Patch"}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if !Evaluate(ctx, dir, script, input).Denied || time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not deny promptly")
	}
	for _, bad := range []string{`{}`, `{"tool_name":"apply_patch","tool_name":"other"}`, `{"x":1} {"x":2}`, strings.Repeat("[", 65) + strings.Repeat("]", 65)} {
		var out bytes.Buffer
		if Run(context.Background(), dir, script, strings.NewReader(bad), &out) != 0 || !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
			t.Fatal("invalid input did not deny")
		}
	}
	for _, body := range []string{"printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecision\":\"allow\"}}'", "printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"updatedInput\":{}}}'"} {
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if !Evaluate(context.Background(), dir, script, input).Denied {
			t.Fatal("ambiguous or rewriting response accepted")
		}
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"allow\"}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(input)
	var out bytes.Buffer
	if Run(context.Background(), dir, script, bytes.NewReader(data), &out) != 0 || strings.Contains(out.String(), "permissionDecision") {
		t.Fatal("local allow became approval grant")
	}
}

func TestGuardEveryPathAndFailClosed(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "guard")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ninput=$(/bin/cat)\ncase \"$input\" in *'\"file_path\":\"'\"$CLAUDE_PROJECT_DIR\"'/protected.txt\"'*) printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"fixture denied\"}}';; esac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	check := func(patch string) Result {
		t.Helper()
		r := Evaluate(context.Background(), dir, script, Input{Event: "PreToolUse", Tool: "apply_patch", Cwd: dir, ToolInput: map[string]any{"command": patch}})
		return r
	}
	if r := check("*** Begin Patch\n*** Add File: allowed.txt\n+safe\n*** End Patch"); r.Denied {
		t.Fatal(r)
	}
	if r := check("*** Begin Patch\n*** Add File: allowed.txt\n+safe\n*** Delete File: protected.txt\n*** End Patch"); !r.Denied {
		t.Fatal("second target bypass")
	}
	if r := check("*** Begin Patch\n*** Update File: allowed.txt\n*** Move to: protected.txt\n@@\n-a\n+b\n*** End Patch"); !r.Denied {
		t.Fatal("rename bypass")
	}
	if r := check("*** Begin Patch\n*** Delete File: ../escape\n*** End Patch"); !r.Denied {
		t.Fatal("outside root accepted")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if r := check("*** Begin Patch\n*** Add File: linked/new.txt\n+x\n*** End Patch"); !r.Denied {
		t.Fatal("symlink accepted")
	}
	if r := check("*** Begin Patch\n*** Add File: linked/../new.txt\n+x\n*** End Patch"); !r.Denied {
		t.Fatal("lexical cleaning hid symlink traversal")
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if r := check("*** Begin Patch\n*** Add File: allowed.txt\n+x\n*** End Patch"); !r.Denied {
		t.Fatal("script failure allowed")
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf broken\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if r := check("*** Begin Patch\n*** Add File: allowed.txt\n+x\n*** End Patch"); !r.Denied {
		t.Fatal("invalid output allowed")
	}
}

func TestGuardRejectsOutputCaseAliases(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "guard")
	for _, body := range []string{
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","PermissionDecision":"allow"}}`,
		`{"HookSpecificOutput":{"hookEventName":"PreToolUse"}}`,
		`{"hookSpecificOutput":{"hookEventName":"PreToolUse","PermissionDecision":"allow"}}`,
	} {
		if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '"+body+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		in := Input{Event: "PreToolUse", Tool: "apply_patch", Cwd: dir, ToolInput: map[string]any{"command": "*** Begin Patch\n*** Delete File: x\n*** End Patch"}}
		if !Evaluate(context.Background(), dir, script, in).Denied {
			t.Fatal("ambiguous casing accepted")
		}
	}
}

func TestGuardRejectsStderrEvenWhenScriptExitsSuccessfully(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "guard")
	for _, body := range []string{
		"printf 'PRIVATE_DEPENDENCY_ERROR' >&2\nexit 0\n",
		"agent_bridge_nonexistent_dependency_for_test\nexit 0\n",
		"printf 'PRIVATE_DEPENDENCY_ERROR' >&2\nprintf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"allow\"}}'\nexit 0\n",
	} {
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
		in := Input{Event: "PreToolUse", Tool: "apply_patch", Cwd: dir, ToolInput: map[string]any{"command": "*** Begin Patch\n*** Add File: protected.txt\n+x\n*** End Patch"}}
		result := Evaluate(context.Background(), dir, script, in)
		if !result.Denied {
			t.Fatal("a swallowed script error bypassed the guard")
		}
		if strings.Contains(result.Reason, "PRIVATE_DEPENDENCY_ERROR") || strings.Contains(result.Context, "PRIVATE_DEPENDENCY_ERROR") {
			t.Fatal("private stderr leaked")
		}
	}
}
