//go:build unix

package sync

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSyncRejectsNonRegularSwap(t *testing.T) {
	for _, mode := range []CompareMode{CompareModTime, CompareSHA1} {
		for _, replacement := range []string{"fifo", "symlink-to-fifo", "regular-file"} {
			t.Run(fmt.Sprintf("mode-%d/%s", mode, replacement), func(t *testing.T) {
				if !syncOpenChild(t) {
					return
				}
				bucket, wire := newSyncWire(t)
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				badPath := filepath.Join(root, "a-bad.txt")
				if err := os.WriteFile(badPath, []byte("bad"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "z-good.txt"), []byte("good"), 0o600); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				replacementPath := filepath.Join(outside, "replacement")
				switch replacement {
				case "fifo":
					if err := syscall.Mkfifo(replacementPath, 0o600); err != nil {
						t.Fatal(err)
					}
				case "symlink-to-fifo":
					fifo := filepath.Join(outside, "pipe")
					if err := syscall.Mkfifo(fifo, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(fifo, replacementPath); err != nil {
						t.Fatal(err)
					}
				case "regular-file":
					if err := os.WriteFile(replacementPath, []byte("outside"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				replaced := false
				walk := func(root string, visit fs.WalkDirFunc) error {
					return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
						if path != badPath || walkErr != nil {
							return visit(path, entry, walkErr)
						}
						info, err := entry.Info()
						if err != nil {
							return err
						}
						// SHA-1 opens during the callback; upload opens after the walk.
						if mode == CompareSHA1 {
							if err := os.Rename(replacementPath, path); err != nil {
								return err
							}
							replaced = true
						}
						if err := visit(path, fs.FileInfoToDirEntry(info), nil); err != nil {
							return err
						}
						if mode != CompareSHA1 {
							if err := os.Rename(replacementPath, path); err != nil {
								return err
							}
							replaced = true
						}
						return nil
					})
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				result, err := syncWithLocalIO(ctx, bucket, "p", root, walk, fileSHA1, WithCompare(mode), WithConcurrency(1))
				if !replaced {
					t.Fatal("fixture never replaced the walked file")
				}
				assertPartialSyncFailure(t, result, err, "a-bad.txt", "[z-good.txt]")
				if err.Error() != "sync: 1 files failed" || result.BytesUploaded != 4 || len(result.Skipped) != 0 {
					t.Fatalf("Sync = (%+v, %v), want one file failure and four uploaded bytes", result, err)
				}
				wire.mu.Lock()
				uploads := slices.Clone(wire.uploads)
				wire.mu.Unlock()
				if len(uploads) != 1 || uploads[0].name != "p/z-good.txt" || string(uploads[0].body) != "good" {
					t.Fatalf("uploads = %+v, want only p/z-good.txt with body good", uploads)
				}
			})
		}
	}
}

func syncOpenChild(t *testing.T) bool {
	t.Helper()
	const marker = "BLAZER_SYNC_OPEN_CHILD"
	if os.Getenv(marker) == t.Name() {
		return true
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A FIFO open ignores context cancellation. Kill and join a regressed child
	// instead of leaving the test suite and its httptest server stuck forever.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	parts := strings.Split(t.Name(), "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	command := exec.CommandContext(ctx, executable, "-test.run="+strings.Join(parts, "/"), "-test.timeout=1m")
	command.Env = append(os.Environ(), marker+"="+t.Name())
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("Sync blocked opening a replaced file beyond the child deadline: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("isolated Sync: %v\n%s", err, output)
	}
	return false
}
