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

package b2

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
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// After a failed upload attempt the retry must wait (backoff),
// not retry immediately.
func TestKnownDefect_UploadRetryWaitsBetweenAttempts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		op    string
		chunk int
		size  int64
	}{
		{"simple upload", "uploadFile", 0, 1e4},
		{"part upload", "uploadPart", 1e4, 3e4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timer := newTestAfter()
			root := &testRoot{
				afterFunc: timer.wait,
				bucketMap: make(map[string]map[string]string),
				errs: &errCont{errMap: map[string]map[int]error{
					tc.op: {0: testError{reupload: true, maxReuploads: 3}, 1: testError{reupload: true, maxReuploads: 3}},
				}},
			}
			client := &Client{backend: &beRoot{b2i: root}}
			b, err := client.NewBucket(newTestCtx(t), "fun", &BucketAttrs{Type: Private})
			if err != nil {
				t.Fatal(err)
			}
			w := b.Object("foo").NewWriter(newTestCtx(t))
			w.ChunkSize = tc.chunk
			if _, err := io.Copy(w, io.LimitReader(zReader{}, tc.size)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			delays := timer.durations()
			if len(delays) != 2 {
				t.Fatalf("%d retry waits recorded, want 2 (one per retry)", len(delays))
			}
			for i, d := range delays {
				if d <= 0 {
					t.Errorf("retry %d waited %v, want a positive backoff", i+1, d)
				}
			}
		})
	}
}

// Two simultaneous uploads of the same object name must both be
// visible in Status().
func TestKnownDefect_ConcurrentSameNameWritersBothInStatus(t *testing.T) {
	bucket, _ := newMappingBucket(t, updateBucketJSON)
	ctx := newTestCtx(t)
	w1 := bucket.Object("same").NewWriter(ctx)
	w2 := bucket.Object("same").NewWriter(ctx)
	if _, err := w1.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := w2.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	got := len(bucket.c.Status().Writers)
	_ = w1.Close()
	_ = w2.Close()
	if got != 2 {
		t.Errorf("Status lists %d writers for two simultaneous uploads of one name, want 2", got)
	}
}

// Resuming with a shorter source must finish with only the parts
// the new source has (no stale leftover parts).
func TestKnownDefect_ResumeShorterSourceDropsStaleParts(t *testing.T) {
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

// A part upload that fails with a bare io.EOF must surface as an
// error from Write/Close, not stall or silently succeed.
func TestKnownDefect_BareEOFFromPartUploadIsReported(t *testing.T) {
	root := &testRoot{
		bucketMap: make(map[string]map[string]string),
		errs: &errCont{errMap: map[string]map[int]error{
			"uploadPart": {0: io.EOF},
		}},
	}
	client := &Client{backend: &beRoot{b2i: root}}
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	b, err := client.NewBucket(parent, "fun", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	w := b.Object("foo").NewWriter(parent)
	w.ChunkSize = 1e4
	type result struct{ werr, cerr error }
	done := make(chan result, 1)
	go func() {
		_, werr := io.Copy(w, io.LimitReader(zReader{}, 5e4))
		done <- result{werr, w.Close()}
	}()
	select {
	case r := <-done:
		if r.werr == nil && r.cerr == nil {
			t.Error("part upload failed with io.EOF but neither Write nor Close reported an error")
		}
	case <-time.After(5 * time.Second):
		cancelParent() // release the stuck goroutine
		<-done
		t.Error("Write/Close blocked after a part upload failed with io.EOF")
	}
}
