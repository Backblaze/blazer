package b2

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAccountInfoFileIOErrorsAndRecovery(t *testing.T) {
	root := t.TempDir()
	store := NewAccountInfoFile(root)
	if _, ok := store.AuthData(); ok || store.Err() == nil {
		t.Fatal("directory read did not report an I/O error")
	}
	parent := filepath.Join(root, "parent-file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store = NewAccountInfoFile(filepath.Join(parent, "session"))
	store.Set(sampleAuthData())
	if store.Err() == nil {
		t.Fatal("write through a non-directory succeeded")
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(private, "session")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	store = NewAccountInfoFile(target)
	store.Set(sampleAuthData())
	if store.Err() == nil {
		t.Fatal("rename over directory succeeded")
	}
	entries, err := os.ReadDir(private)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "session" {
		t.Fatal("failed rename leaked temporary files")
	}
	store = NewAccountInfoFile(filepath.Join(private, "missing"))
	store.Clear()
	store.Clear()
	if store.Err() != nil {
		t.Fatal(store.Err())
	}
	store.Set(sampleAuthData())
	if _, ok := store.AuthData(); !ok {
		t.Fatal("store did not recover")
	}
}

// A failed Set must name its cause and leave the previously stored session
// readable and unchanged (the write is atomic, so nothing partial replaces it).
func TestAccountInfoFileFailedSetNamesCauseAndKeepsOldSession(t *testing.T) {
	good := sampleAuthData()
	tooBig := sampleAuthData()
	tooBig.AuthToken = strings.Repeat("x", maxAccountInfoFileBytes)
	cases := []struct {
		name string
		data AuthData
		want string
	}{
		{"oversized session", tooBig, "exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			private := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(private, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(private, "session")
			store := NewAccountInfoFile(path)
			store.Set(good)
			if err := store.Err(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store.Set(tc.data)
			if err := store.Err(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Err() = %v, want an error containing %q", err, tc.want)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("failed Set modified the previous session file")
			}
			if got, ok := store.AuthData(); !ok || got.AuthToken != good.AuthToken {
				t.Fatalf("AuthData() = %+v, %v, want the original session", got, ok)
			}
		})
	}
}

func TestAccountInfoFileRejectsNonPrivateParentWithoutTouchingOldSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits are not meaningful on Windows")
	}
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "session")
	store := NewAccountInfoFile(path)
	store.Set(sampleAuthData())
	if err := store.Err(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	next := sampleAuthData()
	next.AuthToken = "other-token"
	store.Set(next)
	if err := store.Err(); err == nil || !strings.Contains(err.Error(), "must have mode 0700") {
		t.Fatalf("Err() = %v, want a mode 0700 error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected Set modified the previous session file")
	}
}
