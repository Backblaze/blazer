package sync

import (
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
)

func FuzzShouldTransfer(f *testing.F) {
	f.Add(int64(0), int64(0), int64(0), int64(0), "", "none")
	f.Add(int64(100000001), int64(100000001), int64(1700000000000), int64(1700000000000), "abc", "abc")
	f.Add(int64(100000001), int64(100000002), int64(1700000000000), int64(1700000000001), "abc", "other")
	f.Add(int64(12), int64(12), int64(1700000000000), int64(1700000000000), "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222")
	f.Fuzz(func(t *testing.T, size, destSize, millis, destMillis int64, sha, other string) {
		local := localFile{size: size, modTime: time.UnixMilli(millis), sha1: sha}
		dest := &b2.Attrs{Size: destSize, LastModified: time.UnixMilli(destMillis), SHA1: sha}
		for _, mode := range []CompareMode{CompareModTime, CompareSize, CompareNone, CompareSHA1} {
			if !shouldTransfer(local, nil, mode) {
				t.Fatal("missing file was skipped")
			}
		}
		// Change only the compared field; unrelated metadata must not affect
		// the decision. These properties keep the modes independent.
		sameTime := *dest
		sameTime.LastModified = local.modTime
		if shouldTransfer(local, &sameTime, CompareModTime) {
			t.Fatal("equal modification times transferred because sizes differ")
		}
		sameTime.LastModified = local.modTime.Add(time.Millisecond)
		if !shouldTransfer(local, &sameTime, CompareModTime) {
			t.Fatal("different modification times were skipped")
		}
		sameTime.LastModified = local.modTime.Add(-time.Millisecond)
		if !shouldTransfer(local, &sameTime, CompareModTime) {
			t.Fatal("newer local modification time was skipped")
		}
		sameSize := *dest
		sameSize.Size = local.size
		if shouldTransfer(local, &sameSize, CompareSize) {
			t.Fatal("equal sizes transferred because modification times differ")
		}
		sameSize.Size ^= 1
		if !shouldTransfer(local, &sameSize, CompareSize) {
			t.Fatal("different sizes were skipped")
		}
		// A matching known checksum ignores both size and modification time.
		knownLocal := local
		knownLocal.sha1 = "1111111111111111111111111111111111111111"
		knownDest := *dest
		knownDest.SHA1 = knownLocal.sha1
		if shouldTransfer(knownLocal, &knownDest, CompareSHA1) {
			t.Fatal("matching checksum transferred because other metadata differs")
		}
		dest.SHA1 = other
		wantTransfer := other == "none" || other == "" || sha != other
		if got := shouldTransfer(local, dest, CompareSHA1); got != wantTransfer {
			t.Fatalf("SHA1 comparison: transfer=%t, want %t (size=%d/%d mtime=%d/%d)", got, wantTransfer, size, destSize, millis, destMillis)
		}
		if shouldTransfer(local, dest, CompareNone) {
			t.Fatal("existing file was transferred in none mode")
		}
	})
}
