//go:build !unix

package sync

import (
	"fmt"
	"os"
)

func openSyncFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sync: file %q is not a regular file", path)
	}
	return os.Open(path)
}
