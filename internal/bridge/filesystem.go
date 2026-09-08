package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Snapshot struct {
	Data string `json:"data"`
	Mode uint32 `json:"mode"`
}

func equal(a, b *Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func valid(s *Snapshot) error {
	if s == nil {
		return nil
	}
	if s.Mode > 0777 {
		return fmt.Errorf("invalid file snapshot mode")
	}
	_, err := base64.StdEncoding.Strict().DecodeString(s.Data)
	return err
}
func fingerprint(s *Snapshot) string {
	if s == nil {
		return ""
	}
	// Same compact JSON array and SHA-256 used by the Node v0.2 engine.
	data, _ := json.Marshal([]any{s.Data, s.Mode & 0111})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func encoded(v any) (*Snapshot, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &Snapshot{base64.StdEncoding.EncodeToString(data), 0600}, nil
}
func decode(s *Snapshot, v any) error {
	if s == nil {
		return fmt.Errorf("missing JSON file")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(s.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
func uuid() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func assertSafe(file string) error {
	if !filepath.IsAbs(file) {
		return fmt.Errorf("expected absolute path")
	}
	cursor := filepath.VolumeName(file) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(file, cursor), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", cursor)
		}
	}
	return nil
}
func snapshot(file string) (*Snapshot, error) {
	if err := assertSafe(file); err != nil {
		return nil, err
	}
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = checkRegular(info); err != nil {
		return nil, err
	}
	f, err := openNoFollow(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err = checkRegular(before); err != nil {
		return nil, err
	}
	if !os.SameFile(info, before) {
		return nil, fmt.Errorf("file identity changed while opening; retry")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return nil, fmt.Errorf("file changed while reading; retry")
	}
	return &Snapshot{base64.StdEncoding.EncodeToString(data), uint32(after.Mode().Perm())}, nil
}
func writeSnapshot(file string, s *Snapshot) error {
	if s == nil {
		return fmt.Errorf("missing file snapshot")
	}
	if err := valid(s); err != nil {
		return err
	}
	if err := assertSafe(file); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".agent-bridge-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	data, _ := base64.StdEncoding.DecodeString(s.Data)
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(os.FileMode(s.Mode)); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = assertSafe(file); err != nil {
		return err
	}
	return os.Rename(temp, file)
}
func writeJSON(file string, v any) error {
	s, err := encoded(v)
	if err != nil {
		return err
	}
	return writeSnapshot(file, s)
}
func walk(root string) ([]string, error) {
	if err := assertSafe(root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("expected a skill directory")
	}
	files := []string{}
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", file)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported special file: %s", file)
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		files = append(files, relative)
		return nil
	})
	sort.Strings(files)
	return files, err
}
