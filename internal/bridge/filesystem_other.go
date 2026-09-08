//go:build !darwin && !linux

package bridge

import (
	"fmt"
	"os"
)

func checkRegular(os.FileInfo) error {
	return fmt.Errorf("this version supports macOS and Linux only; filesystem safety has not been validated on this OS")
}
func openNoFollow(string) (*os.File, error) { return nil, fmt.Errorf("unsupported operating system") }
