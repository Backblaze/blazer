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
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Issue #177 R1 requires a shorter resume to fail instead of publishing stale
// trailing parts. The Native API cannot be assumed to finish a subset of parts.
func TestRegression_ResumeShorterSourceIsRejected(t *testing.T) {
	for _, storedParts := range []int{2, 3, 4, 5} {
		t.Run(fmt.Sprintf("%d_stored_parts", storedParts), func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"unexpected-restart"}`)
			part := bytes.Repeat([]byte("r"), 5_000_000)
			digest := fmt.Sprintf("%x", sha1.Sum(part))
			var parts []map[string]any
			for number := 1; number <= storedParts; number++ {
				parts = append(parts, map[string]any{"partNumber": number, "contentLength": len(part), "contentSha1": digest})
			}
			body, err := json.Marshal(map[string]any{"parts": parts, "nextPartNumber": nil})
			if err != nil {
				t.Fatal(err)
			}
			srv.Reply("b2_list_parts", http.StatusOK, string(body))
			w := bucket.Object("resume").NewWriter(newTestCtx(t))
			w.Resume, w.ChunkSize = true, len(part)
			w.UseFileBuffer, w.FileBufferDir = true, t.TempDir()
			if _, err := w.ReadFrom(bytes.NewReader(bytes.Repeat(part, 3))); err != nil {
				t.Fatal(err)
			}
			if entries, readErr := os.ReadDir(w.FileBufferDir); readErr != nil || len(entries) == 0 {
				t.Fatalf("active file buffers = %v, error = %v; want a buffer before Close", entries, readErr)
			}
			err = w.Close()
			if entries, readErr := os.ReadDir(w.FileBufferDir); readErr != nil || len(entries) != 0 {
				t.Errorf("file buffers after Close = %v, error = %v; want empty directory", entries, readErr)
			}
			if storedParts > 3 {
				if err == nil || !strings.Contains(err.Error(), "source has fewer parts") {
					t.Errorf("Close = %v, want shorter-source error", err)
				}
				for _, call := range []string{"b2_finish_large_file", "b2_start_large_file", "upload_part"} {
					if calls := srv.Calls(call); len(calls) != 0 {
						t.Errorf("%s calls = %d, want 0 for a rejected shorter source", call, len(calls))
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := srv.Calls("b2_finish_large_file")
			if len(calls) != 1 {
				t.Fatalf("finish calls = %d, want 1", len(calls))
			}
			hashes, ok := calls[0].JSON["partSha1Array"].([]any)
			if !ok || len(hashes) != 3 {
				t.Fatalf("finished hashes = %v, want 3 source hashes", calls[0].JSON["partSha1Array"])
			}
			for index, hash := range hashes {
				if hash != digest {
					t.Errorf("part %d hash = %v, want %s", index+1, hash, digest)
				}
			}
			if got, want := len(srv.Calls("upload_part")), 3-storedParts; got != want {
				t.Errorf("part uploads = %d, want %d", got, want)
			}
		})
	}
}

// A shortened source can reuse some parts and upload others. Only its parts and
// bytes may be accepted; stale trailing parts must prevent finishing.
func TestResumeShorterSourceWithNewFinalPart(t *testing.T) {
	for _, mode := range []string{"ReadFrom", "non-seekable", "Write", "file buffer"} {
		t.Run(mode, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"unexpected-restart"}`)
			part := bytes.Repeat([]byte("r"), 5_000_000)
			digest := fmt.Sprintf("%x", sha1.Sum(part))
			var parts []map[string]any
			// Part 3 was never uploaded; parts 4 and 5 belong to the old longer source.
			for _, n := range []int{1, 2, 4, 5} {
				parts = append(parts, map[string]any{"partNumber": n, "contentLength": len(part), "contentSha1": digest})
			}
			body, err := json.Marshal(map[string]any{"parts": parts, "nextPartNumber": nil})
			if err != nil {
				t.Fatal(err)
			}
			srv.Reply("b2_list_parts", http.StatusOK, string(body))
			final := bytes.Repeat([]byte("x"), 123)
			payload := append(bytes.Repeat(part, 2), final...)
			obj := bucket.Object("resume")
			w := obj.NewWriter(newTestCtx(t))
			w.Resume, w.ChunkSize, w.ConcurrentUploads = true, len(part), 3
			bufferDir := t.TempDir()
			var n int64
			switch mode {
			case "Write":
				var written int
				written, err = w.Write(payload)
				n = int64(written)
			case "non-seekable":
				n, err = w.ReadFrom(struct{ io.Reader }{bytes.NewReader(payload)})
			default:
				if mode == "file buffer" {
					w.UseFileBuffer, w.FileBufferDir = true, bufferDir
				}
				n, err = w.ReadFrom(bytes.NewReader(payload))
			}
			if err != nil || n != int64(len(payload)) {
				t.Fatalf("source copy = %d, %v; want %d, nil", n, err, len(payload))
			}
			if w.UseFileBuffer {
				if entries, readErr := os.ReadDir(bufferDir); readErr != nil || len(entries) == 0 {
					t.Fatalf("active file buffers = %v, error = %v; want a buffer before Close", entries, readErr)
				}
			}
			if err := w.Close(); err == nil || !strings.Contains(err.Error(), "source has fewer parts") {
				t.Errorf("Close = %v, want shorter-source error", err)
			}
			for _, call := range []string{"b2_finish_large_file", "b2_start_large_file"} {
				if calls := srv.Calls(call); len(calls) != 0 {
					t.Errorf("%s calls = %d, want 0 for a rejected shorter source", call, len(calls))
				}
			}
			if entries, readErr := os.ReadDir(bufferDir); readErr != nil || len(entries) != 0 {
				t.Errorf("file buffers after Close = %v, error = %v; want empty directory", entries, readErr)
			}
			uploads := srv.Calls("upload_part")
			if len(uploads) != 1 {
				t.Fatalf("part uploads = %d, want only new part 3", len(uploads))
			}
			if uploads[0].Header.Get("X-Bz-Part-Number") != "3" || !bytes.Equal(uploads[0].Body, final) {
				t.Fatalf("uploaded part %s with %d bytes, want part 3 with the new final bytes", uploads[0].Header.Get("X-Bz-Part-Number"), len(uploads[0].Body))
			}
		})
	}
}
