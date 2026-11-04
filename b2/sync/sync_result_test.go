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

package sync

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestSyncSortsFailureResults(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	lastErr := errors.New("last failure")
	firstErr := errors.New("first failure")
	walk := func(root string, visit fs.WalkDirFunc) error {
		if err := visit(filepath.Join(root, "z-last"), nil, lastErr); err != nil {
			return err
		}
		return visit(filepath.Join(root, "a-first"), nil, firstErr)
	}

	result, err := syncWithLocalIO(context.Background(), bucket, "p", root, walk, fileSHA1)
	if err == nil || err.Error() != "sync: 2 files failed" {
		t.Fatalf("syncWithLocalIO error = %v, want %q", err, "sync: 2 files failed")
	}
	if result == nil || len(result.Failed) != 2 ||
		result.Failed[0].Rel != "a-first" || result.Failed[0].Err != firstErr ||
		result.Failed[1].Rel != "z-last" || result.Failed[1].Err != lastErr {
		t.Fatalf("failed results = %+v, want sorted failures", result)
	}
}

func TestSyncReturnsCancellationAfterWalk(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	walk := func(string, fs.WalkDirFunc) error {
		cancel()
		return nil
	}

	result, err := syncWithLocalIO(ctx, bucket, "p", root, walk, fileSHA1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("syncWithLocalIO error = %v, want %v", err, context.Canceled)
	}
	if result == nil || len(result.Uploaded) != 0 || len(result.Skipped) != 0 || len(result.Failed) != 0 {
		t.Fatalf("cancelled result = %+v, want no file results", result)
	}
}
