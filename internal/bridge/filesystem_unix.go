//go:build darwin || linux

package bridge

import (
	"fmt"
	"os"
	"syscall"
)

func checkRegular(info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot inspect file link count")
	}
	if stat.Nlink > 1 {
		return fmt.Errorf("hard-linked files are not supported")
	}
	return nil
}
func openNoFollow(file string) (*os.File, error) {
	return os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
