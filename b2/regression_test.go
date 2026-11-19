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
