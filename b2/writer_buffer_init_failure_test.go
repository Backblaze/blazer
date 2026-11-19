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
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// A Writer whose buffer cannot be created must report the error from Write and
// Close; the usual `defer w.Close()` must not panic.
func TestWriterCloseAfterBufferInitFailure(t *testing.T) {
	for _, cachedURL := range []bool{false, true} {
		name := "without cached upload URL"
		if cachedURL {
			name = "with cached upload URL"
		}
		t.Run(name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			if cachedURL {
				srv.Reply("upload_file", 200, `{"fileId":"first","fileName":"first","action":"upload"}`)
				first := bucket.Object("first").NewWriter(context.Background())
				if _, err := first.Write([]byte("payload")); err != nil {
					t.Fatal(err)
				}
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
			}
			before := len(srv.Calls("upload_file"))
			w := bucket.Object("o").NewWriter(context.Background())
			w.UseFileBuffer = true
			w.FileBufferDir = filepath.Join(t.TempDir(), "missing")
			_, writeErr := w.Write([]byte("x"))
			if writeErr == nil {
				t.Fatal("Write: want an error for an unusable FileBufferDir")
			}
			if err := w.Close(); !errors.Is(err, writeErr) {
				t.Fatalf("Close = %v, want buffer error %v", err, writeErr)
			}
			if got := len(srv.Calls("upload_file")); got != before {
				t.Errorf("upload_file calls = %d, want %d after rejected buffer initialization", got, before)
			}
		})
	}
}
