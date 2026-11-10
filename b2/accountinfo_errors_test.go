package b2

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAccountInfoFileIOErrorsAndRecovery(t *testing.T) {
	root := t.TempDir()
	readParent := filepath.Join(root, "private-read")
	if err := os.Mkdir(readParent, 0o700); err != nil {
		t.Fatal(err)
	}
	readPath := filepath.Join(readParent, "read-directory")
	// Pass the POSIX file-mode check so this case reaches Read on the directory.
	// Windows skips the mode check; reading the directory is still an I/O error.
	if err := os.Mkdir(readPath, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(readPath, 0o700); err != nil {
			t.Error(err)
		}
	})
	native, err := os.Open(readPath)
	if err != nil {
		t.Fatal(err)
	}
	_, nativeErr := native.Read(make([]byte, 1))
	if err := native.Close(); err != nil {
		t.Fatal(err)
	}
	var wantReadErr *os.PathError
	if !errors.As(nativeErr, &wantReadErr) || wantReadErr.Op != "read" {
		t.Fatalf("native directory read = %v, want a read PathError", nativeErr)
	}
	store := NewAccountInfoFile(readPath)
	_, ok := store.AuthData()
	err = store.Err()
	var readErr *os.PathError
	if ok || !errors.As(err, &readErr) || readErr.Op != "read" || readErr.Path != readPath {
		t.Fatalf("directory read: present=%t error=%v, want a read PathError for %q", ok, err, readPath)
	}
	if !errors.Is(err, wantReadErr.Err) || errors.Is(err, ErrInvalidAccountInfo) {
		t.Fatalf("read error = %v, want preserved filesystem error, not invalid cached data", err)
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
	chmodErr error
	writeErr error
	syncErr  error
	closeErr error
}

func (f *recordingAccountInfoFile) Chmod(mode os.FileMode) error {
	if f.chmodErr != nil {
		return f.chmodErr
	}
	return f.File.Chmod(mode)
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
	if err := f.File.Close(); err != nil {
		return err
	}
	return f.closeErr
}

func TestAccountInfoFilePersistence(t *testing.T) {
	syncErr := errors.New("session sync failed")
	writeErr := errors.New("session write failed")
	cleanupErr := errors.New("temporary file cleanup failed")
	chmodErr := errors.New("session chmod failed")
	closeErr := errors.New("session close failed")
	for _, test := range []struct {
		name          string
		chmodErr      error
		writeErr      error
		syncErr       error
		closeErr      error
		cleanupErr    error
		wantErr       error
		wantExactErr  bool
		wantErrorText []string
		wantEvents    []string
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
		{
			name:          "chmod and close failure",
			chmodErr:      chmodErr,
			closeErr:      closeErr,
			wantErr:       closeErr,
			wantErrorText: []string{"chmod:", chmodErr.Error(), "close:", closeErr.Error()},
			wantEvents:    []string{"close", "remove"},
		},
		{
			name:         "chmod failure",
			chmodErr:     chmodErr,
			wantErr:      chmodErr,
			wantExactErr: true,
			wantEvents:   []string{"close", "remove"},
		},
		{
			name: "cleanup failure after replacement", cleanupErr: cleanupErr, wantErr: cleanupErr,
			wantEvents: []string{"write", "sync", "close", "rename", "remove"},
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
					return &recordingAccountInfoFile{
						File: file, events: &events, chmodErr: test.chmodErr,
						writeErr: test.writeErr, syncErr: test.syncErr, closeErr: test.closeErr,
					}, nil
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
			err := store.writeWith(sampleAuthData(), ops)
			if !errors.Is(err, test.wantErr) {
				t.Errorf("write error = %v, want %v", err, test.wantErr)
			}
			if test.wantExactErr && err != test.wantErr {
				t.Errorf("write error = %v, want plain %v", err, test.wantErr)
			}
			for _, text := range test.wantErrorText {
				if err == nil || !strings.Contains(err.Error(), text) {
					t.Errorf("write error = %q, want it to contain %q", err, text)
				}
			}
			if !reflect.DeepEqual(events, test.wantEvents) {
				t.Errorf("file operations = %v, want %v", events, test.wantEvents)
			}
			// Failures before the rename (write, sync, chmod, close) preserve the
			// old session; cleanup errors happen after replacement, so that case
			// must contain the new session.
			if test.writeErr != nil || test.syncErr != nil || test.chmodErr != nil || test.closeErr != nil {
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
		destDir   bool
		renameErr error
		wantErr   error
		wantCalls int
	}{
		{goos: "windows", renameErr: sharingErr, wantCalls: 2},
		{goos: "linux", renameErr: renameErr, wantErr: renameErr, wantCalls: 1},
		{goos: "darwin", renameErr: renameErr, wantErr: renameErr, wantCalls: 1},
		{goos: "windows", destDir: true, renameErr: renameErr, wantErr: renameErr, wantCalls: 1},
	} {
		name := test.goos
		if test.destDir {
			name += " directory"
		}
		t.Run(name, func(t *testing.T) {
			tempPath, destPath := filepath.Join(t.TempDir(), "new"), filepath.Join(t.TempDir(), "previous")
			if test.destDir {
				if err := os.Mkdir(destPath, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			calls, clockCalls := 0, 0
			rename := func(tmp, dest string) error {
				calls++
				if tmp != tempPath || dest != destPath {
					t.Errorf("rename paths = %q, %q; want %q, %q", tmp, dest, tempPath, destPath)
				}
				if calls == 1 || test.destDir {
					return test.renameErr
				}
				return nil
			}
			now := func() time.Time {
				clockCalls++
				return time.Unix(int64(clockCalls), 0)
			}
			if err := renameReplaceWith(tempPath, destPath, rename, test.goos, now); !errors.Is(err, test.wantErr) {
				t.Errorf("rename error = %v, want %v", err, test.wantErr)
			}
			if calls != test.wantCalls {
				t.Errorf("rename calls = %d, want %d", calls, test.wantCalls)
			}
			if (test.goos != "windows" || test.destDir) && clockCalls != 0 {
				t.Errorf("rename entered retry deadline: clock calls = %d, want 0", clockCalls)
			}
		})
	}
}

func TestAccountInfoRenameRetriesUntilSuccessOrDeadline(t *testing.T) {
	sharingErr := errors.New("sharing violation")
	lastErr := errors.New("last sharing violation")
	for _, succeed := range []bool{true, false} {
		name := "deadline returns last error"
		if succeed {
			name = "third attempt succeeds"
		}
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "session")
			calls := 0
			rename := func(tmp, target string) error {
				if tmp != "temporary" || target != dest {
					t.Fatalf("rename(%q, %q), want temporary, %q", tmp, target, dest)
				}
				calls++
				if calls < 3 {
					return sharingErr
				}
				if succeed {
					return nil
				}
				return lastErr
			}
			clock := time.Unix(0, 0)
			now := func() time.Time {
				clock = clock.Add(500 * time.Millisecond)
				return clock
			}
			wantErr := lastErr
			if succeed {
				wantErr = nil
			}
			if err := renameReplaceWith("temporary", dest, rename, "windows", now); !errors.Is(err, wantErr) {
				t.Errorf("rename error = %v, want %v", err, wantErr)
			}
			wantCalls := 4
			if succeed {
				wantCalls = 3
			}
			if calls != wantCalls {
				t.Errorf("rename calls = %d, want %d", calls, wantCalls)
			}
		})
	}
}
