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
			wantTransfer := mode == CompareSHA1 && (sha == "" || sha == "none")
			if got := shouldTransfer(local, dest, mode); got != wantTransfer {
				t.Fatalf("mode %d: transfer=%t, want %t", mode, got, wantTransfer)
			}
		}
		dest.SHA1 = other
		wantTransfer := other == "none" || other == "" || sha != other
		if shouldTransfer(local, dest, CompareSHA1) != wantTransfer {
			t.Fatal("hash difference not respected")
		}
		if shouldTransfer(local, dest, CompareNone) {
			t.Fatal("existing file was transferred in none mode")
		}
	})
}
