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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ReadFrom from a positioned reader reports and uploads only the
// bytes after the position. The live test in integration_test.go currently
// expects the whole size (250) for pos 50; that expectation must change
// together with the fix. This offline test checks the returned count and the
// payload.
func TestKnownDefect_PositionedReaderReportsRemainingBytes(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	data := bytes.Repeat([]byte("z"), 250)
	r := bytes.NewReader(data)
	if _, err := r.Seek(50, 0); err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("pos").NewWriter(newTestCtx(t))
	n, err := w.ReadFrom(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if n != 200 {
		t.Errorf("ReadFrom at position 50 of 250 returned n=%d, want 200", n)
	}
	if body := stripSHA1Trailer(srv.Last(t, "upload_file").Body); len(body) != 200 {
		t.Errorf("uploaded %d bytes, want 200", len(body))
	}
}

// stripSHA1Trailer removes the 40-char hex SHA-1 that simple uploads append.
func stripSHA1Trailer(b []byte) []byte {
	if len(b) >= 40 {
		return b[:len(b)-40]
	}
	return b
}

// Close after a failed buffer setup must return an error, not panic.
func TestKnownDefect_CloseAfterBufferSetupFailureDoesNotPanic(t *testing.T) {
	bucket, _ := newMappingBucket(t, updateBucketJSON)
	w := bucket.Object("obj").NewWriter(newTestCtx(t))
	w.UseFileBuffer = true
	w.FileBufferDir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := w.Write([]byte("hello")); err == nil {
		t.Fatal("expected buffer-creation error from Write")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Close panicked: %v", r)
		}
	}()
	if err := w.Close(); err == nil {
		t.Fatal("Close returned nil after a failed Write")
	}
}

// Closing a writer that never received data must remove it from the
// client's writer list and release its (file) buffer.
func TestKnownDefect_EmptyWriterCloseDeregistersWriter(t *testing.T) {
	bucket, _ := newMappingBucket(t, updateBucketJSON)
	dir := t.TempDir()
	w := bucket.Object("empty").NewWriter(newTestCtx(t))
	w.UseFileBuffer = true
	w.FileBufferDir = dir
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(bucket.c.Status().Writers); got != 0 {
		t.Errorf("%d writer(s) still listed after Close of an empty writer, want 0", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("%d temp file(s) left in buffer dir, want 0", len(entries))
	}
}

// Write after Close must be rejected with ErrClosed (small file).
func TestKnownDefect_WriteAfterCloseIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"small file", []byte("hello")},
		{"empty writer", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, _ := newMappingBucket(t, updateBucketJSON)
			w := bucket.Object("obj").NewWriter(newTestCtx(t))
			if len(tc.payload) > 0 {
				if _, err := w.Write(tc.payload); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Write after Close panicked: %v", r)
				}
			}()
			n, err := w.Write([]byte("lost"))
			if !errors.Is(err, ErrClosed) {
				t.Fatalf("Write after Close = (%d, %v), want ErrClosed", n, err)
			}
		})
	}
}

// If the source of ReadFrom fails half way, Close must not save the
// partial data as a complete object.
func TestKnownDefect_PartialSourceIsNotCommittedByClose(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	w := bucket.Object("partial").NewWriter(newTestCtx(t))
	srcErr := errors.New("source failed")
	if _, err := w.ReadFrom(&failingSourceReader{data: []byte("partial"), err: srcErr}); !errors.Is(err, srcErr) {
		t.Fatalf("ReadFrom err = %v, want %v", err, srcErr)
	}
	closeErr := w.Close()
	if n := len(srv.Calls("upload_file")); n != 0 {
		t.Errorf("Close committed %d upload(s) of partial content after the source failed", n)
	}
	if closeErr == nil {
		t.Error("Close returned nil after the source failed half way")
	}
}

// When a large upload cannot start (ReadFrom from a seekable
// source), Close must not save only the first chunk as the whole object.
func TestKnownDefect_FailedLargeStartIsRecordedAndCloseDoesNotCommitFirstChunk(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_start_large_file", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"cannot start"}`)
	w := bucket.Object("large").NewWriter(newTestCtx(t))
	w.ChunkSize = 10
	if _, err := w.ReadFrom(bytes.NewReader(bytes.Repeat([]byte("x"), 25))); err == nil {
		t.Fatal("ReadFrom returned nil although b2_start_large_file failed")
	}
	closeErr := w.Close()
	if n := len(srv.Calls("upload_file")); n != 0 {
		t.Errorf("Close saved %d object(s) (first chunk only) after the large upload failed to start", n)
	}
	if closeErr == nil {
		t.Error("Close returned nil after the large upload failed to start")
	}
}

// ReadFrom with a partly read ReadSeeker uploads only the rest.
func TestKnownDefect_ReadFromHonoursReaderPosition(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	data := []byte("0123456789abcde")
	r := bytes.NewReader(data)
	if _, err := r.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("pos").NewWriter(newTestCtx(t))
	n, err := w.ReadFrom(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body := stripSHA1Trailer(srv.Last(t, "upload_file").Body)
	if want := data[8:]; !bytes.Equal(body, want) {
		t.Errorf("uploaded %q (%d bytes), want %q (%d bytes); ReadFrom reported n=%d", body, len(body), want, len(want), n)
	}
}

// ReadFrom with an os pipe (not seekable) must not turn into a
// successful empty object. Either the data is uploaded or an error is reported.
func TestKnownDefect_PipeSourceDoesNotProduceEmptyObject(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pr.Close() }()
	go func() {
		_, _ = pw.Write([]byte("hello"))
		_ = pw.Close()
	}()
	w := bucket.Object("pipe").NewWriter(newTestCtx(t))
	_, readErr := w.ReadFrom(pr)
	closeErr := w.Close()
	uploads := srv.Calls("upload_file")
	if readErr == nil && closeErr == nil {
		if len(uploads) != 1 {
			t.Fatalf("no error but %d uploads", len(uploads))
		}
		if body := stripSHA1Trailer(uploads[0].Body); string(body) != "hello" {
			t.Errorf("object content %q, want %q", body, "hello")
		}
		return
	}
	if len(uploads) != 0 {
		t.Errorf("ReadFrom err=%v, Close err=%v, but %d object(s) were saved", readErr, closeErr, len(uploads))
	}
	if closeErr == nil {
		t.Errorf("ReadFrom failed (%v) but Close returned nil", readErr)
	}
}

// A negative ChunkSize must fail with an error, not crash.
func TestKnownDefect_NegativeChunkSizeFailsWithoutPanic(t *testing.T) {
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

// Status() on a client built from a saved session (no request
// made yet) must not panic.
func TestKnownDefect_StatusBeforeAnyRequestDoesNotPanic(t *testing.T) {
	store := &InMemoryAccountInfo{}
	store.Set(sampleAuthData())
	client, err := NewClient(context.Background(), "key-id", "secret", WithAccountInfo(store))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Status() panicked before any request: %v", r)
		}
	}()
	si := client.Status()
	if si == nil {
		t.Fatal("nil status")
	}
}

// A resume hash mismatch must not leave the chunk's temp file behind.
func TestKnownDefect_ResumeMismatchRemovesTempFile(t *testing.T) {
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

// The writer's internal context must be cancelled once the writer
// is closed successfully, so it does not stay attached to a long-lived parent.
func TestKnownDefect_WriterContextCancelledAfterSuccessfulClose(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"simple upload", 5}, {"large upload", 25}} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"lid","fileName":"obj"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"lid","fileName":"obj","action":"upload","uploadTimestamp":1}`)
			w := bucket.Object("obj").NewWriter(context.Background())
			w.ChunkSize = 10
			if _, err := w.Write(bytes.Repeat([]byte("x"), tc.size)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if w.ctx.Err() == nil {
				t.Error("writer context still live after successful Close")
			}
		})
	}
}

type failingSourceReader struct {
	data []byte
	err  error
}

func (f *failingSourceReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}
