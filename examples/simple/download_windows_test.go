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

//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDownloadToOpenDestinationPreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "destination")
	if err := os.WriteFile(dst, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		t.Fatal(err)
	}
	// Allow normal reads and writes, but deny sharing for delete/rename.
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(handle) })
	err = downloadToFile(strings.NewReader("replacement"), dst)
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) || linkErr.Op != "rename" {
		t.Fatalf("download to a locked destination: got %v, want a rename error", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "original" {
		t.Errorf("destination = %q, %v; want original contents", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("directory entries = %v, %v; want only destination", entries, err)
	}
}
