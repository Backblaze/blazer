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
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// A negative ChunkSize must fail with an error, not crash.
func TestRegression_NegativeChunkSizeFailsWithoutPanic(t *testing.T) {
	bucket, _ := newMappingBucket(t, updateBucketJSON)
	w := bucket.Object("neg").NewWriter(newTestCtx(t))
	w.ChunkSize = -1
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("negative ChunkSize panicked: %v", r)
		}
	}()
	_, werr := w.Write([]byte("hello"))
	cerr := w.Close()
	if werr == nil && cerr == nil {
		t.Fatal("negative ChunkSize accepted without error")
	}
}

// A resume hash mismatch must not leave the chunk's temp file behind.
func TestRegression_ResumeMismatchRemovesTempFile(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
	bad := fmt.Sprintf("%x", sha1.Sum([]byte("different content")))
	body, _ := json.Marshal(map[string]any{"parts": []map[string]any{{"partNumber": 1, "contentLength": 10, "contentSha1": bad}}, "nextPartNumber": nil})
	srv.Reply("b2_list_parts", http.StatusOK, string(body))
	dir := t.TempDir()
	w := bucket.Object("resume").NewWriter(newTestCtx(t))
	w.Resume = true
	w.ChunkSize = 10
	w.UseFileBuffer = true
	w.FileBufferDir = dir
	_, _ = w.Write(bytes.Repeat([]byte("a"), 25))
	if err := w.Close(); err == nil {
		t.Fatal("Close returned nil despite a resume hash mismatch")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%d temp file(s) left after a resume hash mismatch, want 0", len(entries))
	}
}
