package sync

import (
	"os"
	"syscall"
)

func openSyncFile(path string) (*os.File, error) {
	// Open the final reparse point itself, rather than following a replacement
	// symlink between the walk and open. openWalkedFile then rejects its type.
	return os.OpenFile(path, os.O_RDONLY|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}
