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

//go:build (!linux && !darwin && !windows) || android || ios

package sync

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

func TestSyncUnsupportedPlatform(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "upload"
		var options []Option
		if dryRun {
			name = "dry-run"
			options = []Option{WithDryRun()}
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			sim := b2sim.New()
			sim.Seed("bucket")
			client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(sim))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.Bucket(ctx, "bucket")
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("data"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := sim.Calls()
			result, err := Sync(ctx, bucket, "p", root, options...)
			if result != nil || !errors.Is(err, errors.ErrUnsupported) {
				t.Fatalf("Sync = (%+v, %v), want nil result and errors.ErrUnsupported", result, err)
			}
			if !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
				t.Fatalf("platform error %q lacks GOOS/GOARCH", err)
			}
			if after := sim.Calls(); !maps.Equal(before, after) {
				t.Fatalf("Sync made requests on an unsupported platform: before=%v after=%v", before, after)
			}
		})
	}
}

func TestOpenSyncFileUnsupportedPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openSyncFile(path)
	if file != nil {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("opened a file on an unsupported platform")
	}
	if !errors.Is(err, errors.ErrUnsupported) || !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("openSyncFile error = %v, want errors.ErrUnsupported with GOOS/GOARCH", err)
	}
}
