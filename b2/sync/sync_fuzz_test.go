package sync

import (
	"github.com/Backblaze/blazer/b2"
	"testing"
	"time"
)

func FuzzShouldTransfer(f *testing.F) {
	f.Add(int64(0), int64(0), "", "none")
	f.Add(int64(100000001), int64(1700000000000), "abc", "abc")
	f.Fuzz(func(t *testing.T, size, millis int64, sha, other string) {
		local := localFile{size: size, modTime: time.UnixMilli(millis), sha1: sha}
		dest := &b2.Attrs{Size: size, LastModified: local.modTime, SHA1: sha}
		for _, mode := range []CompareMode{CompareModTime, CompareSize, CompareNone, CompareSHA1} {
			if !shouldTransfer(local, nil, mode) {
				t.Fatal("missing file was skipped")
			}
			if shouldTransfer(local, dest, mode) {
				t.Fatal("identical file was transferred")
			}
		}
		dest.SHA1 = other
		if other != "none" && shouldTransfer(local, dest, CompareSHA1) != (sha != other) {
			t.Fatal("hash difference not respected")
		}
		if shouldTransfer(local, dest, CompareNone) {
			t.Fatal("existing file was transferred in none mode")
		}
	})
}
