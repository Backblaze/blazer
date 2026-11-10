//go:build unix

package sync

import (
	"os"
	"syscall"
)

func openSyncFile(path string) (*os.File, error) {
	// Check the opened descriptor in openWalkedFile. A replaced FIFO must not
	// block before that check, and a replaced symlink must not be followed.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
