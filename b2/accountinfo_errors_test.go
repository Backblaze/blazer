package b2

import (
	"os"
	"path/filepath"
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
