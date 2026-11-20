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

// Regression tests that pass on the current code and protect behaviour that
// is already correct.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// authorizeCheckingServer authorizes only Basic key-id:good-secret and answers
// every other call with an expired-token error, so a cached session is always
// followed by a re-authorization.
func authorizeCheckingServer(t *testing.T) (*httptest.Server, *int, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	auths := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			mu.Lock()
			auths++
			mu.Unlock()
			if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("key-id:good-secret")) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":401,"code":"bad_auth_token","message":"bad credentials"}`))
				return
			}
			_, _ = w.Write([]byte(`{"accountId":"account","authorizationToken":"fresh","apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"recommendedPartSize":100000000,"apiUrl":"` + srv.URL + `","downloadUrl":"` + srv.URL + `","s3ApiUrl":"` + srv.URL + `","allowed":{"buckets":null,"capabilities":["listBuckets"],"namePrefix":null}}}}`))
		default:
			if r.Header.Get("Authorization") != "fresh" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":401,"code":"expired_auth_token","message":"expired"}`))
				return
			}
			_, _ = w.Write([]byte(`{"buckets":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &auths, &mu
}

// A cached session for the same
// key ID is accepted without a network call, but a wrong secret must never
// yield a working client: the first re-authorization fails and the call errors.
func TestRegression_CachedSessionWithWrongSecretFailsAtReauthorization(t *testing.T) {
	srv, auths, mu := authorizeCheckingServer(t)
	store := &InMemoryAccountInfo{}
	data := sampleAuthData()
	data.APIURL, data.DownloadURL, data.S3URL = srv.URL, srv.URL, srv.URL
	store.Set(data)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "key-id", "wrong-secret", APIBase(srv.URL), WithAccountInfo(store))
	if err != nil {
		t.Fatalf("NewClient with a cached session: %v", err)
	}
	mu.Lock()
	if *auths != 0 {
		t.Errorf("authorize calls at NewClient = %d, want 0 (cached session is reused)", *auths)
	}
	mu.Unlock()
	if _, err := client.ListBuckets(ctx); err == nil {
		t.Fatal("ListBuckets succeeded with a wrong secret; the bad secret must surface when re-authorization is needed")
	}
	mu.Lock()
	defer mu.Unlock()
	if *auths < 1 {
		t.Errorf("authorize calls after the expired token = %d, want at least 1", *auths)
	}
}

// The session file must be exactly
// mode 0600; any other mode is rejected before any network call, 0600 works.
func TestRegression_SessionFileModeMustBeExactly0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not authoritative on Windows")
	}
	for _, tc := range []struct {
		mode os.FileMode
		ok   bool
	}{{0o400, false}, {0o640, false}, {0o644, false}, {0o600, true}} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private", "account.json")
			store := NewAccountInfoFile(path)
			store.Set(sampleAuthData())
			if err := store.Err(); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			_, ok := store.AuthData()
			if ok != tc.ok {
				t.Errorf("mode %v: AuthData ok=%v err=%v, want ok=%v", tc.mode, ok, store.Err(), tc.ok)
			}
			if !tc.ok && (store.Err() == nil || errors.Is(store.Err(), ErrInvalidAccountInfo)) {
				t.Errorf("mode %v: err=%v, want a non-recoverable permission error", tc.mode, store.Err())
			}
		})
	}
}

// blockingDownload makes every download request block until release is closed.
func blockingDownload(t *testing.T) *Bucket {
	t.Helper()
	srv, bucket := newSHA1WireBucket(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // runs before the server closes
	srv.Handle("slow", func(r b2wiretest.Request) b2wiretest.Response {
		if r.Method == http.MethodHead {
			return b2wiretest.Response{Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {"id"}}}
		}
		<-release
		return b2wiretest.RangeDownload("AAAAAAAA", r.Header.Get("Range"))
	})
	return bucket
}

// readerCall describes one way a Read can meet a closed or cancelled reader.
type readerCall struct {
	name string
	run  func(bucket *Bucket, ctx context.Context) any // returns a recovered panic, if any
}

func recoverRead(r *Reader) (panicked any) {
	defer func() { panicked = recover() }()
	_, _ = r.Read(make([]byte, 4))
	return nil
}

func newSlowReader(bucket *Bucket, ctx context.Context) *Reader {
	reader := bucket.Object("slow").NewReader(ctx)
	reader.ChunkSize = 4
	reader.ConcurrentDownloads = 1
	return reader
}

var readerCloseCancelCalls = []readerCall{
	{"Read after Close", func(bucket *Bucket, ctx context.Context) any {
		r := newSlowReader(bucket, ctx)
		_ = r.Close()
		return recoverRead(r)
	}},
	{"Read after parent context cancelled", func(bucket *Bucket, ctx context.Context) any {
		cctx, cancel := context.WithCancel(ctx)
		r := newSlowReader(bucket, cctx)
		cancel()
		p := recoverRead(r)
		_ = r.Close()
		return p
	}},
	{"Close racing a waiting Read", func(bucket *Bucket, ctx context.Context) any {
		r := newSlowReader(bucket, ctx)
		done := make(chan any, 1)
		before := chunkWaitersParked()
		go func() { done <- recoverRead(r) }()
		if !awaitCond(func() bool { return newChunkWaiterParked(before) }) {
			// The race was never set up: Close would not meet a waiting Read.
			closeWithin(r, 3*time.Second)
			return "the Read never parked waiting for a chunk, so Close did not race it"
		}
		if !closeWithin(r, 3*time.Second) {
			return "Close did not return"
		}
		select {
		case p := <-done:
			return p
		case <-time.After(3 * time.Second):
			return "Read did not return after Close"
		}
	}},
}

// Close or cancel meeting a Read must not crash on a missing chunk.
func TestRegression_CloseOrCancelAroundReadDoesNotCrash(t *testing.T) {
	bucket := blockingDownload(t)
	for _, call := range readerCloseCancelCalls {
		t.Run(call.name, func(t *testing.T) {
			for i := 0; i < 80; i++ {
				// A fresh context per iteration: an earlier iteration cannot
				// consume the deadline of the one being set up.
				if p := call.run(bucket, newTestCtx(t)); p != nil {
					t.Fatalf("iteration %d: %v", i, p)
				}
			}
		})
	}
}

// closeWithin runs r.Close and reports whether it returned within d, so a
// Close that blocks fails the test instead of hanging the suite.
func closeWithin(r *Reader, d time.Duration) bool {
	closed := make(chan struct{})
	go func() {
		_ = r.Close()
		close(closed)
	}()
	select {
	case <-closed:
		return true
	case <-time.After(d):
		return false
	}
}

// chunkWaitersParked returns the ids of goroutines that are parked in the
// condition wait inside (*Reader).curChunk, that is, a Read that is really
// waiting for a chunk (not one that has only just been started).
func chunkWaitersParked() map[string]bool {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	ids := map[string]bool{}
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "(*Reader).curChunk") && strings.Contains(g, "sync.(*Cond).Wait") {
			ids[strings.SplitN(g, " [", 2)[0]] = true
		}
	}
	return ids
}

// newChunkWaiterParked reports whether a waiter that is not in known is parked.
func newChunkWaiterParked(known map[string]bool) bool {
	for id := range chunkWaitersParked() {
		if !known[id] {
			return true
		}
	}
	return false
}

// awaitCond yields the processor until cond holds or a 3 second bound passes.
// It replaces fixed sleeps: the caller proceeds as soon as the state it needs
// (a Read blocked in curChunk, or no waiter left) is observable.
func awaitCond(cond func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		runtime.Gosched()
		time.Sleep(100 * time.Microsecond) // poll interval, not a timing assumption
	}
	return true
}

func newTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// These predicates are shared with the staged tests so unrelated failures
// cannot make a known defect look fixed.
func unsupportedRetentionError(err error) bool {
	return errors.Is(err, errors.ErrUnsupported)
}

func changedVersionError(err error) bool {
	if err == nil {
		return false
	}
	// A contextual wrapper must not turn an unrelated I/O cause into an
	// identity-change error. No version-change sentinel exists in the SDK yet.
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "changed") && (strings.Contains(m, "version") || strings.Contains(m, "file id") || strings.Contains(m, "fileid"))
}

func validEmptyKeyPage(next string, err error) bool {
	return next == "next-id" && err == nil
}

func TestRegression_KnownDefectErrorsAreSpecific(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check func(error) bool
		err   error
		want  bool
	}{
		{"retention rejection", unsupportedRetentionError, fmt.Errorf("default retention during creation: %w", errors.ErrUnsupported), true},
		{"retention sentinel", unsupportedRetentionError, errors.ErrUnsupported, true},
		{"retention words alone", unsupportedRetentionError, errors.New("b2: default retention is not supported during creation"), false},
		{"retention wrapped transport", unsupportedRetentionError, fmt.Errorf("b2: default retention not supported: %w", io.ErrUnexpectedEOF), false},
		{"retention nil", unsupportedRetentionError, nil, false},
		{"retention transport", unsupportedRetentionError, io.ErrUnexpectedEOF, false},
		{"retention unrelated", unsupportedRetentionError, errors.New("unscripted endpoint"), false},
		{"retention lookup", unsupportedRetentionError, errors.New("default retention lookup failed"), false},
		{"version changed", changedVersionError, errors.New("b2: file version changed during download"), true},
		{"version wrapped transport", changedVersionError, fmt.Errorf("file version changed: %w", io.ErrUnexpectedEOF), false},
		{"version nil", changedVersionError, nil, false},
		{"version checksum", changedVersionError, errors.New("SHA-1 mismatch"), false},
		{"version transport", changedVersionError, io.ErrUnexpectedEOF, false},
		{"version cancelled", changedVersionError, context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check(tc.err); got != tc.want {
				t.Errorf("accepted error %v = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRegression_EmptyKeyPageRequiresSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		next string
		err  error
		want bool
	}{
		{"cursor", "next-id", nil, true},
		{"unexpected error with cursor", "next-id", errors.New("unrelated failure"), false},
		{"EOF with cursor", "next-id", io.EOF, false},
		{"missing cursor", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validEmptyKeyPage(tc.next, tc.err); got != tc.want {
				t.Errorf("accepted empty page (next=%q, err=%v) = %v, want %v", tc.next, tc.err, got, tc.want)
			}
		})
	}
}

const (
	knownDefectChunkSize   = 5_000_000
	knownDefectUploadBytes = 10_000_001 // two minimum-size parts and a one-byte final part
)

// The retry fixture must itself obey B2's 5,000,000-byte non-final-part minimum:
// https://www.backblaze.com/apidocs/b2-upload-part
func TestRegression_KnownDefectMultipartFixtureUsesValidParts(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"fixture-large"}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"fixture-large","fileName":"fixture","action":"upload"}`)
	w := bucket.Object("fixture").NewWriter(newTestCtx(t))
	w.ChunkSize = knownDefectChunkSize
	if _, err := w.Write(make([]byte, knownDefectUploadBytes)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	parts := srv.Calls("upload_part")
	if len(parts) != 3 {
		t.Fatalf("part uploads = %d, want 3", len(parts))
	}
	want := map[string]int{"1": 5_000_000, "2": 5_000_000, "3": 1}
	for _, part := range parts {
		number := part.Header.Get("X-Bz-Part-Number")
		size, ok := want[number]
		if !ok {
			t.Fatalf("unexpected or duplicate part number %q", number)
		}
		delete(want, number)
		body := part.Body
		if part.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" {
			if len(body) < 40 {
				t.Fatalf("part %s has no complete SHA-1 trailer", number)
			}
			body = body[:len(body)-40]
		}
		if got := len(body); got != size {
			t.Errorf("part %s bytes = %d, want %d", number, got, size)
		}
	}
}

// bytes after the position. The live test in integration_test.go currently
// expects the whole size (250) for pos 50; that expectation must change
// together with the fix. This offline test checks the returned count and the
// payload.
func TestRegression_PositionedReaderReportsRemainingBytes(t *testing.T) {
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
func TestRegression_CloseAfterBufferSetupFailureDoesNotPanic(t *testing.T) {
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
func TestRegression_EmptyWriterCloseDeregistersWriter(t *testing.T) {
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
func TestRegression_WriteAfterCloseIsRejected(t *testing.T) {
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
func TestRegression_PartialSourceIsNotCommittedByClose(t *testing.T) {
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
func TestRegression_FailedLargeStartIsRecordedAndCloseDoesNotCommitFirstChunk(t *testing.T) {
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
func TestRegression_ReadFromHonoursReaderPosition(t *testing.T) {
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
func TestRegression_PipeSourceDoesNotProduceEmptyObject(t *testing.T) {
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

// Status() on a client built from a saved session (no request
// made yet) must not panic.
func TestRegression_StatusBeforeAnyRequestDoesNotPanic(t *testing.T) {
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

// The writer's internal context must be cancelled once the writer
// is closed successfully, so it does not stay attached to a long-lived parent.
func TestRegression_WriterContextCancelledAfterSuccessfulClose(t *testing.T) {
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

// Reader.Verify before any Read (or after a failed key check)
// must return (nil, false), not panic.
func TestRegression_VerifyBeforeReadDoesNotPanic(t *testing.T) {
	_, bucket := newSHA1WireBucket(t)
	for _, tc := range []struct {
		name string
		mk   func() *Reader
	}{
		{"before any Read", func() *Reader { return bucket.Object("o").NewReader(newTestCtx(t)) }},
		{"after a rejected SSE-C key", func() *Reader {
			bad := &SSECKey{} // a zero key is rejected by validation
			r := bucket.Object("o").NewReader(newTestCtx(t), bad)
			_, _ = r.Read(make([]byte, 1))
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.mk()
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Verify panicked: %v", p)
				}
			}()
			if err, ok := r.Verify(); err != nil || ok {
				t.Errorf("Verify = (%v, %v), want (nil, false)", err, ok)
			}
		})
	}
}

func (f *failingSourceReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}
