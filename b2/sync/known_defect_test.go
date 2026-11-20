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

//go:build knowndefect

package sync

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A nil Option must be rejected with an error, not crash.
func TestKnownDefect_NilOptionReturnsErrorWithoutPanic(t *testing.T) {
	bucket, _ := newSyncWire(t)
	root := t.TempDir()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Sync panicked on a nil option: %v", r)
		}
	}()
	if _, err := Sync(context.Background(), bucket, "p", root, nil); err == nil {
		t.Error("Sync accepted a nil option without error")
	}
}

// A dry run must report the same failure for a name B2 would
// reject (over 1024 bytes) as the real run does, not list it as a planned upload.
func TestKnownDefect_DryRunFlagsNamesB2WouldReject(t *testing.T) {
	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	for _, name := range []string{"a-long-filename.txt", "good.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefix := strings.Repeat("p", 1010) // prefix + "/" + name exceeds 1024 bytes for the long name only
	result, err := Sync(context.Background(), bucket, prefix, root, WithConcurrency(1), WithDryRun())
	if result == nil {
		t.Fatalf("nil result, err=%v", err)
	}
	if len(result.Failed) != 1 || result.Failed[0].Rel != "a-long-filename.txt" || fmt.Sprint(result.Uploaded) != "[good.txt]" || err == nil {
		t.Errorf("dry run Uploaded=%v Failed=%+v err=%v; want Uploaded=[good.txt], one failure for a-long-filename.txt and an error (as in the real run)", result.Uploaded, result.Failed, err)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if len(wire.files) != 0 {
		t.Errorf("dry run wrote %d objects", len(wire.files))
	}
}
