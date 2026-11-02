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
	"testing"
)

// A Writer whose buffer cannot be created must report the error from Write and
// Close; the usual `defer w.Close()` must not panic.
func TestWriterCloseAfterBufferInitFailure(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	_ = srv
	w := bucket.Object("o").NewWriter(context.Background())
	w.UseFileBuffer = true
	w.FileBufferDir = "/nonexistent/dir/xyz"
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("Write: want an error for an unusable FileBufferDir")
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close: want the buffer error, got nil")
	}
}
