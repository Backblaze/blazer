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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	fifoHelperEnv    = "BLAZER_SYNC_FIFO_HELPER_ROOT"
	fifoHelperMarker = "FIFO_SYNC_RESULT "
)

// fifoSyncOutcome is what the helper process reports back to the parent test.
type fifoSyncOutcome struct {
	Err      string
	Uploaded []string
	Failed   int
	Stored   []string // object names the fake B2 server holds
}

// Non-regular files in the tree (here a FIFO) are skipped without blocking or
// failing the run, and the regular files around them are still uploaded.
//
// Opening a FIFO that has no writer blocks in the operating system, and a
// cancelled context does not interrupt that open. If the guard against
// non-regular files were removed, an in-process Sync would hang the whole test
// binary until the global go test timeout. So the scenario runs in a helper
// process (this test binary re-executed) that the parent kills after a bound.
func TestRegression_NonRegularFilesAreSkippedAndRegularFilesUploaded(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "regular.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create a FIFO here: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess_SyncWithFIFO$", "-test.count=1")
	cmd.Env = append(os.Environ(), fifoHelperEnv+"="+root)
	cmd.WaitDelay = 5 * time.Second // reap the killed child and its pipes
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("Sync did not finish within 30s with a FIFO in the tree (blocked opening the FIFO?); helper output:\n%s", out)
	}
	if err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, out)
	}
	var outcome fifoSyncOutcome
	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, fifoHelperMarker); ok {
			if err := json.Unmarshal([]byte(rest), &outcome); err != nil {
				t.Fatalf("decoding the helper result %q: %v", rest, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("the helper process reported no result (was the helper test discovered?):\n%s", out)
	}
	if outcome.Err != "" {
		t.Fatalf("Sync with a FIFO in the tree: %s", outcome.Err)
	}
	if len(outcome.Uploaded) != 1 || outcome.Uploaded[0] != "regular.txt" || outcome.Failed != 0 {
		t.Errorf("Uploaded=%v Failed=%d, want only regular.txt uploaded and no failures", outcome.Uploaded, outcome.Failed)
	}
	if got := strings.Join(outcome.Stored, ","); got != "p/regular.txt" {
		t.Errorf("objects stored on the server = %q, want only p/regular.txt (the FIFO must not be uploaded)", got)
	}
}

// TestHelperProcess_SyncWithFIFO is not a test: it is the body that the parent
// test runs in a child process. It does nothing unless the parent set the
// environment variable.
func TestHelperProcess_SyncWithFIFO(t *testing.T) {
	root := os.Getenv(fifoHelperEnv)
	if root == "" {
		t.Skip("helper for TestRegression_NonRegularFilesAreSkippedAndRegularFilesUploaded; run only by that test")
	}
	bucket, wire := newSyncWire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := Sync(ctx, bucket, "p", root, WithConcurrency(1))
	outcome := fifoSyncOutcome{Uploaded: result.Uploaded, Failed: len(result.Failed)}
	if err != nil {
		outcome.Err = fmt.Sprintf("%v (result %+v)", err, result)
	}
	wire.mu.Lock()
	for name := range wire.files {
		outcome.Stored = append(outcome.Stored, name)
	}
	wire.mu.Unlock()
	sort.Strings(outcome.Stored)
	b, _ := json.Marshal(outcome)
	fmt.Println(fifoHelperMarker + string(b))
}
