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
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Resuming with a shorter source must finish with only the parts
// the new source has (no stale leftover parts).
func TestRegression_ResumeShorterSourceDropsStaleParts(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
	part := bytes.Repeat([]byte("r"), 5_000_000)
	digest := fmt.Sprintf("%x", sha1.Sum(part))
	var parts []map[string]any
	for n := 1; n <= 5; n++ {
		parts = append(parts, map[string]any{"partNumber": n, "contentLength": 5_000_000, "contentSha1": digest})
	}
	body, _ := json.Marshal(map[string]any{"parts": parts, "nextPartNumber": nil})
	srv.Reply("b2_list_parts", http.StatusOK, string(body))
	w := bucket.Object("resume").NewWriter(newTestCtx(t))
	w.Resume = true
	w.ChunkSize = 5_000_000
	if _, err := w.ReadFrom(bytes.NewReader(bytes.Repeat(part, 3))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	hashes, _ := srv.Last(t, "b2_finish_large_file").JSON["partSha1Array"].([]any)
	if len(hashes) != 3 {
		t.Errorf("finish sent %d part hashes for a 3-chunk source, want 3", len(hashes))
	}
}

// A shortened source can reuse some parts and upload others. Only its parts and
// bytes may be included in the final file, regardless of how the source is read.
func TestResumeShorterSourceWithNewFinalPart(t *testing.T) {
	for _, mode := range []string{"ReadFrom", "non-seekable", "Write", "file buffer"} {
		t.Run(mode, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
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
					w.UseFileBuffer, w.FileBufferDir = true, t.TempDir()
				}
				n, err = w.ReadFrom(bytes.NewReader(payload))
			}
			if err != nil || n != int64(len(payload)) {
				t.Fatalf("source copy = %d, %v; want %d, nil", n, err, len(payload))
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			wantHashes := []string{digest, digest, fmt.Sprintf("%x", sha1.Sum(final))}
			b2wiretest.AssertJSON(t, srv.Last(t, "b2_finish_large_file"), "partSha1Array", wantHashes)
			uploads := srv.Calls("upload_part")
			if len(uploads) != 1 {
				t.Fatalf("part uploads = %d, want only new part 3", len(uploads))
			}
			if uploads[0].Header.Get("X-Bz-Part-Number") != "3" || !bytes.Equal(uploads[0].Body, final) {
				t.Fatalf("uploaded part %s with %d bytes, want part 3 with the new final bytes", uploads[0].Header.Get("X-Bz-Part-Number"), len(uploads[0].Body))
			}
			if got := obj.f.size(); got != int64(len(payload)) {
				t.Errorf("finished size = %d, want %d", got, len(payload))
			}
		})
	}
}
