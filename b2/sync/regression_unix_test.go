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

//go:build (linux && !android) || (darwin && !ios)

package sync

// Regression tests that pass on the current code and protect behaviour that
// is already correct.

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Non-regular files in the
// tree (here a FIFO) are skipped without blocking or failing the run, and the
// regular files around them are still uploaded.
func TestRegression_NonRegularFilesAreSkippedAndRegularFilesUploaded(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "regular.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := Sync(ctx, bucket, "p", root, WithConcurrency(1))
	if err != nil {
		t.Fatalf("Sync with a FIFO in the tree: %v (result %+v)", err, result)
	}
	if len(result.Uploaded) != 1 || result.Uploaded[0] != "regular.txt" || len(result.Failed) != 0 {
		t.Errorf("Uploaded=%v Failed=%+v, want only regular.txt uploaded and no failures", result.Uploaded, result.Failed)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if _, ok := wire.files["p/pipe"]; ok {
		t.Error("the FIFO was uploaded")
	}
}
