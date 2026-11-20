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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSyncRejectsNilOptionsBeforeRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{name: "only", opts: []Option{nil}},
		{name: "first", opts: []Option{nil, WithConcurrency(1)}},
		{name: "last", opts: []Option{WithConcurrency(1), nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, wire := newSyncWire(t)
			wire.mu.Lock()
			before := len(wire.requests)
			wire.mu.Unlock()
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("Sync panicked for a nil option: %v", value)
				}
			}()
			result, err := Sync(context.Background(), bucket, "p", t.TempDir(), tc.opts...)
			if result != nil || err == nil || !strings.Contains(err.Error(), "nil option") {
				t.Errorf("Sync = (%+v, %v), want nil result and nil-option error", result, err)
			}
			wire.mu.Lock()
			defer wire.mu.Unlock()
			if got := len(wire.requests) - before; got != 0 {
				t.Errorf("Sync made %d requests for a nil option, want 0", got)
			}
		})
	}
}

func TestSyncObjectNameByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string
		bytes  int
		valid  bool
	}{
		{name: "ascii-1023", prefix: strings.Repeat("p", 1017), bytes: 1023, valid: true},
		{name: "ascii-1024", prefix: strings.Repeat("p", 1018), bytes: 1024, valid: true},
		{name: "ascii-1025", prefix: strings.Repeat("p", 1019), bytes: 1025},
		{name: "unicode-1024", prefix: strings.Repeat("界", 339) + "p", bytes: 1024, valid: true},
		{name: "unicode-1025", prefix: strings.Repeat("界", 339) + "pp", bytes: 1025},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dryRun), func(t *testing.T) {
				if got := len(tc.prefix + "/a.txt"); got != tc.bytes {
					t.Fatalf("fixture is %d bytes, want %d", got, tc.bytes)
				}
				bucket, wire := newSyncWire(t)
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				var opts []Option
				if dryRun {
					opts = append(opts, WithDryRun())
				}
				wire.mu.Lock()
				before := len(wire.requests)
				wire.mu.Unlock()
				result, err := Sync(context.Background(), bucket, tc.prefix, root, opts...)
				if result == nil {
					t.Fatalf("nil result: %v", err)
				}
				if tc.valid {
					if err != nil || len(result.Failed) != 0 || !slices.Equal(result.Uploaded, []string{"a.txt"}) {
						t.Errorf("Sync = (%+v, %v), want a.txt uploaded and no failures", result, err)
					}
				} else if err == nil || len(result.Uploaded) != 0 || len(result.Failed) != 1 ||
					result.Failed[0].Rel != "a.txt" || result.Failed[0].Err == nil || !strings.Contains(result.Failed[0].Err.Error(), "1024") {
					t.Errorf("Sync = (%+v, %v), want a.txt failed for exceeding 1024 bytes", result, err)
				}
				wire.mu.Lock()
				defer wire.mu.Unlock()
				wantFiles := 0
				if tc.valid && !dryRun {
					wantFiles = 1
				}
				if len(wire.files) != wantFiles {
					t.Errorf("stored files = %d, want %d", len(wire.files), wantFiles)
				}
				if !tc.valid {
					for _, request := range wire.requests[before:] {
						if request.path == "/upload" || strings.HasSuffix(request.path, "/b2_get_upload_url") || strings.HasSuffix(request.path, "/b2_start_large_file") {
							t.Errorf("invalid name reached upload request %s", request.path)
						}
					}
				}
			})
		}
	}
}
