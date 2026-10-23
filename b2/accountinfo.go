// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// AccountInfo stores an authorized session. It never stores the application
// key secret; callers still supply that key to NewClient for reauthorization.
type AccountInfo interface {
	Set(AuthData)
	AuthData() (AuthData, bool)
	Clear()
}

// InMemoryAccountInfo is the default, concurrency-safe session store.
type InMemoryAccountInfo struct {
	mu   sync.Mutex
	data AuthData
	ok   bool
}

// Set replaces the in-memory session.
func (a *InMemoryAccountInfo) Set(data AuthData) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data = cloneAuthData(data)
	a.ok = true
}

// AuthData returns a detached snapshot of the stored session.
func (a *InMemoryAccountInfo) AuthData() (AuthData, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneAuthData(a.data), a.ok
}

// Clear removes the stored session.
func (a *InMemoryAccountInfo) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data, a.ok = AuthData{}, false
}

func cloneAuthData(data AuthData) AuthData {
	data.AllowedBuckets = append([]string(nil), data.AllowedBuckets...)
	data.AllowedCapabilities = append([]string(nil), data.AllowedCapabilities...)
	return data
}

// AccountInfoFile stores one authorized session in JSON. The file contains a
// live authorization token, but never the application key secret. Keep the
// file and its parent directory private. File I/O failures are available via
// Err because AccountInfo's Set and Clear methods have no error result.
type AccountInfoFile struct {
	path string
	mu   sync.Mutex
	err  error
}

// NewAccountInfoFile selects an opt-in session file. Its parent directory is
// created with mode 0700; an existing parent must already be private.
func NewAccountInfoFile(path string) *AccountInfoFile {
	return &AccountInfoFile{path: path}
}

// Err reports the last file operation error, if any.
func (a *AccountInfoFile) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// Set writes the session atomically with file mode 0600.
func (a *AccountInfoFile) Set(data AuthData) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = a.write(data)
}

func (a *AccountInfoFile) write(data AuthData) (resultErr error) {
	parent := filepath.Dir(a.path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("account info: parent %q is not a directory", parent)
	}
	// Unix mode bits do not reflect access on Windows (directory privacy is
	// governed by filesystem ACLs there, and Stat reports a synthetic 0777), so
	// only enforce the private-parent requirement where the bits are meaningful.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("account info: parent directory %q must have mode 0700", parent)
	}
	file, err := os.CreateTemp(parent, ".account-info-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(file.Name()); err != nil && !os.IsNotExist(err) && resultErr == nil {
			resultErr = err
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("chmod: %v; close: %w", err, closeErr)
		}
		return err
	}
	writeErr := json.NewEncoder(file).Encode(cloneAuthData(data))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return renameReplace(file.Name(), a.path)
}

// renameReplace atomically moves tmp onto dest. os.Rename replaces an existing
// file atomically on both Unix and Windows (MoveFileEx with REPLACE_EXISTING),
// so a concurrent reader always observes either the old or the new complete
// file -- never a partial one. On Windows the move can still fail transiently
// with a sharing violation while another process briefly holds the destination;
// those clear on their own, so retry. A directory is never replaced.
func renameReplace(tmp, dest string) error {
	err := os.Rename(tmp, dest)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	if info, statErr := os.Lstat(dest); statErr == nil && info.IsDir() {
		return err
	}
	// Windows: a concurrent reader or writer can briefly hold dest, so the
	// atomic move fails with a transient sharing violation. Retry until it
	// clears, bounded by a deadline.
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if err = os.Rename(tmp, dest); err == nil {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return err
}

// AuthData reads the latest persisted session.
func (a *AccountInfoFile) AuthData() (AuthData, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	content, err := os.ReadFile(a.path)
	if os.IsNotExist(err) {
		a.err = nil
		return AuthData{}, false
	}
	if err != nil {
		a.err = err
		return AuthData{}, false
	}
	var data AuthData
	if err := json.Unmarshal(content, &data); err != nil {
		// A corrupt or incomplete cache is not an authorization failure.
		// Treat it as a miss so NewClient can authorize and replace it.
		a.err = nil
		return AuthData{}, false
	}
	a.err = nil
	return cloneAuthData(data), true
}

// Clear removes the persisted session before a 401-triggered reauthorization.
func (a *AccountInfoFile) Clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = os.Remove(a.path)
	if os.IsNotExist(a.err) {
		a.err = nil
	}
}
