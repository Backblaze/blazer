//go:build !unix && !windows

package sync

import (
	"fmt"
	"os"
)

func openSyncFile(path string) (*os.File, error) {
	// These platforms lack the Unix/Windows atomic-open protection. Keep their
	// existing file support, with a pre-open type check and post-open identity checks.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sync: file %q is not a regular file", path)
	}
	return os.Open(path)
}
