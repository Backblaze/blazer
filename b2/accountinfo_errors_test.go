package b2

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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

type recordingAccountInfoFile struct {
	*os.File
	events   *[]string
	writeErr error
	syncErr  error
}

func (f *recordingAccountInfoFile) Write(p []byte) (int, error) {
	*f.events = append(*f.events, "write")
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func (f *recordingAccountInfoFile) Sync() error {
	*f.events = append(*f.events, "sync")
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.File.Sync()
}

func (f *recordingAccountInfoFile) Close() error {
	*f.events = append(*f.events, "close")
	return f.File.Close()
}

func TestAccountInfoFilePersistence(t *testing.T) {
	syncErr := errors.New("session sync failed")
	writeErr := errors.New("session write failed")
	cleanupErr := errors.New("temporary file cleanup failed")
	for _, test := range []struct {
		name       string
		writeErr   error
		syncErr    error
		cleanupErr error
		wantErr    error
		wantEvents []string
	}{
		{
			name:       "write sync close rename",
			wantEvents: []string{"write", "sync", "close", "rename", "remove"},
		},
		{
			name: "sync failure", syncErr: syncErr, wantErr: syncErr,
			wantEvents: []string{"write", "sync", "close", "remove"},
		},
		{
			name: "primary error survives cleanup", writeErr: writeErr, cleanupErr: cleanupErr, wantErr: writeErr,
			wantEvents: []string{"write", "close", "remove"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "session.json")
			const previous = "{\"AccountID\":\"previous\",\"AuthToken\":\"previous-token\"}\n"
			if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
				t.Fatal(err)
			}
			var events []string
			var tempName string
			ops := accountInfoFileOps{
				createTemp: func(dir, pattern string) (accountInfoTempFile, error) {
					file, err := os.CreateTemp(dir, pattern)
					if err != nil {
						return nil, err
					}
					tempName = file.Name()
					t.Cleanup(func() { _ = file.Close() })
					return &recordingAccountInfoFile{File: file, events: &events, writeErr: test.writeErr, syncErr: test.syncErr}, nil
				},
				remove: func(name string) error {
					events = append(events, "remove")
					if name != tempName {
						t.Fatalf("cleanup path = %q, want temporary file %q", name, tempName)
					}
					if test.cleanupErr != nil {
						return test.cleanupErr
					}
					return os.Remove(name)
				},
				rename: func(old, dest string) error {
					events = append(events, "rename")
					if old != tempName || dest != path {
						t.Fatalf("rename paths = %q, %q; want temporary file and %q", old, dest, path)
					}
					return os.Rename(old, dest)
				},
			}
			store := NewAccountInfoFile(path)
			if err := store.writeWith(sampleAuthData(), ops); !errors.Is(err, test.wantErr) {
				t.Errorf("write error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(events, test.wantEvents) {
				t.Errorf("file operations = %v, want %v", events, test.wantEvents)
			}
			if test.wantErr != nil {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != previous {
					t.Errorf("previous session changed after failure: got %q, want %q", content, previous)
				}
			} else if got, ok := store.AuthData(); !ok || got.AccountID != "account" || got.AuthToken != "session-token" {
				t.Errorf("saved session = account %q, token %q, ok %t; want account, session-token, true", got.AccountID, got.AuthToken, ok)
			}
		})
	}
}

func TestAccountInfoRenamePlatform(t *testing.T) {
	sharingErr := errors.New("sharing violation")
	renameErr := errors.New("rename denied")
	for _, test := range []struct {
		goos      string
		renameErr error
		wantErr   error
		wantCalls int
	}{
		{goos: "windows", renameErr: sharingErr, wantCalls: 2},
		{goos: "linux", renameErr: renameErr, wantErr: renameErr, wantCalls: 1},
		{goos: "darwin", renameErr: renameErr, wantErr: renameErr, wantCalls: 1},
	} {
		t.Run(test.goos, func(t *testing.T) {
			tmp, dest := filepath.Join(t.TempDir(), "new"), filepath.Join(t.TempDir(), "previous")
			calls, clockCalls := 0, 0
			rename := func(old, new string) error {
				calls++
				if old != tmp || new != dest {
					t.Errorf("rename paths = %q, %q; want %q, %q", old, new, tmp, dest)
				}
				if calls == 1 {
					return test.renameErr
				}
				return nil
			}
			now := func() time.Time {
				clockCalls++
				return time.Unix(0, 0)
			}
			if err := renameReplaceWith(tmp, dest, rename, test.goos, now); !errors.Is(err, test.wantErr) {
				t.Errorf("rename error = %v, want %v", err, test.wantErr)
			}
			if calls != test.wantCalls {
				t.Errorf("rename calls = %d, want %d", calls, test.wantCalls)
			}
			if test.goos != "windows" && clockCalls != 0 {
				t.Errorf("non-Windows rename entered retry deadline: clock calls = %d, want 0", clockCalls)
			}
		})
	}
}
