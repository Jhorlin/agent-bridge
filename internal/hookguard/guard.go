package hookguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Input struct {
	Event     string         `json:"hook_event_name"`
	Tool      string         `json:"tool_name"`
	Cwd       string         `json:"cwd"`
	ToolInput map[string]any `json:"tool_input"`
}
type Result struct {
	Denied  bool
	Reason  string
	Context string
}

func deny() Result {
	return Result{Denied: true, Reason: "Agent Bridge file guard could not validate this edit; review its input, script, and paths."}
}
func inside(root, p string) bool {
	r, e := filepath.Rel(root, p)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Canonicalize existing components by inode identity, including filesystem case
// aliases, and reject symlinks. A hook check is not a filesystem race sandbox.
func safePath(p string) (string, error) {
	if !filepath.IsAbs(p) || !unambiguousPath(p) {
		return "", fmt.Errorf("absolute path required")
	}
	p = filepath.Clean(p)
	root := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(p, root), string(filepath.Separator))
	for i, part := range parts {
		next := filepath.Join(root, part)
		info, err := os.Lstat(next)
		if os.IsNotExist(err) {
			return filepath.Join(append([]string{root}, parts[i:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink unsupported")
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.Name() == part {
				break
			}
			if !strings.EqualFold(entry.Name(), part) {
				continue
			}
			candidate, e := entry.Info()
			if e == nil && os.SameFile(info, candidate) {
				part = entry.Name()
				break
			}
		}
		root = filepath.Join(root, part)
	}
	return root, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("hook output too large")
	}
	return b.Buffer.Write(p)
}

// Evaluate executes only the explicitly supplied reviewed executable, directly
// (never a shell expression), once per target. Errors deny instead of granting.
func Evaluate(ctx context.Context, project, executable string, in Input) Result {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return deny()
	}
	project, err := safePath(project)
	if err != nil {
		return deny()
	}
	info, err := os.Stat(project)
	if err != nil || !info.IsDir() {
		return deny()
	}
	cwd, err := safePath(in.Cwd)
	if err != nil || !inside(project, cwd) {
		return deny()
	}
	executable, err = safePath(executable)
	if err != nil {
		return deny()
	}
	info, err = os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return deny()
	}
	if in.Event != "PreToolUse" || in.Tool != "apply_patch" || len(in.ToolInput) != 1 {
		return deny()
	}
	patch, ok := in.ToolInput["command"].(string)
	if !ok {
		return deny()
	}
	paths, err := PatchTargets(patch)
	if err != nil {
		return deny()
	}
	for i, path := range paths {
		if ctx.Err() != nil {
			return deny()
		}
		// Validate before Join/Clean can erase a symlink/../ traversal. Native
		// patch application may resolve it differently from lexical cleaning.
		if !unambiguousPath(path) {
			return deny()
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		paths[i], err = safePath(path)
		if err != nil || !inside(project, paths[i]) {
			return deny()
		}
	}
	result := Result{}
	for _, path := range paths {
		// Retain the original patch as context, but give legacy path guards the
		// absolute file_path they inspect. Never run the patch itself here.
		payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Edit", "cwd": cwd, "tool_input": map[string]any{"file_path": path, "path": path, "command": patch}}
		data, _ := json.Marshal(payload)
		cmd := exec.CommandContext(ctx, executable)
		cmd.Dir = cwd
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "CLAUDE_PROJECT_DIR=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "CLAUDE_PROJECT_DIR="+project)
		cmd.Stdin = bytes.NewReader(data)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = time.Second
		output := &limitedBuffer{limit: 64 << 10}
		stderr := &limitedBuffer{limit: 64 << 10}
		cmd.Stdout = output
		cmd.Stderr = stderr
		err := cmd.Run()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if err != nil || stderr.Len() != 0 {
			return deny()
		}
		if len(bytes.TrimSpace(output.Bytes())) == 0 {
			continue
		}
		if uniqueJSON(output.Bytes()) != nil || !exactOutputKeys(output.Bytes()) {
			return deny()
		}
		var response struct {
			Specific struct {
				Event    string `json:"hookEventName"`
				Decision string `json:"permissionDecision"`
				Reason   string `json:"permissionDecisionReason"`
				Context  string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		d := json.NewDecoder(bytes.NewReader(output.Bytes()))
		d.DisallowUnknownFields()
		if err := d.Decode(&response); err != nil {
			return deny()
		}
		var extra any
		if !errors.Is(d.Decode(&extra), io.EOF) {
			return deny()
		}
		if response.Specific.Event != "PreToolUse" {
			return deny()
		}
		switch response.Specific.Decision {
		case "deny":
			return Result{Denied: true, Reason: response.Specific.Reason}
		case "", "allow": // An allow never becomes an approval grant on the host.
		default:
			return deny()
		}
		result.Context += response.Specific.Context + "\n"
		if len(result.Context) > 64<<10 {
			return deny()
		}
	}
	return result
}

func Run(ctx context.Context, project, executable string, in io.Reader, out io.Writer) int {
	data, err := io.ReadAll(io.LimitReader(in, (1<<20)+1))
	var input Input
	result := deny()
	if err == nil && len(data) <= 1<<20 && uniqueJSON(data) == nil && json.Unmarshal(data, &input) == nil {
		result = Evaluate(ctx, project, executable, input)
	}
	specific := map[string]any{"hookEventName": "PreToolUse"}
	if result.Denied {
		specific["permissionDecision"] = "deny"
		specific["permissionDecisionReason"] = result.Reason
	} else if strings.TrimSpace(result.Context) != "" {
		specific["additionalContext"] = result.Context
	}
	if err := json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": specific}); err != nil {
		return 2
	}
	return 0
}
