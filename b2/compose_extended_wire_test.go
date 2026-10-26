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
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const composeMinPartSize = 5e6

type composeUpload struct {
	number int
	sha1   string
	size   int
}

type composeWire struct {
	t *testing.T

	srv *httptest.Server
	mu  sync.Mutex

	failCopyPart        int
	failUploadPart      bool
	failFirstUploadPart bool
	uploadURLCalls      int
	failFinish          bool
	copySizeOverride    int64
	failCancel          bool
	starts              []map[string]any
	calls               []string
	copies              []map[string]any
	uploads             []composeUpload
	finishes            []map[string]any
}

func newComposeWire(t *testing.T) *composeWire {
	t.Helper()
	w := &composeWire{t: t}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serveHTTP))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *composeWire) serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/b2api/v4/b2_authorize_account":
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
	case "/b2api/v4/b2_list_buckets":
		_, _ = fmt.Fprint(rw, `{"buckets":[]}`)
	case "/b2api/v4/b2_create_bucket":
		_, _ = fmt.Fprint(rw, noReplicationBucketJSON)
	case "/b2api/v4/b2_start_large_file":
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.t.Errorf("decode start: %v", err)
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "t" {
			w.t.Errorf("start method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		w.mu.Lock()
		w.starts = append(w.starts, body)
		w.mu.Unlock()
		w.addCall("start")
		_, _ = fmt.Fprint(rw, `{"fileId":"large"}`)
	case "/b2api/v4/b2_get_upload_part_url":
		w.mu.Lock()
		w.uploadURLCalls++
		urlCalls := w.uploadURLCalls
		w.mu.Unlock()
		token := "part-token"
		if w.failFirstUploadPart {
			token = fmt.Sprintf("part-token-%d", urlCalls)
		}
		_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":%q}`, w.srv.URL+"/part", token)
	case "/part":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.t.Error(err)
		}
		number, err := strconv.Atoi(r.Header.Get("X-Bz-Part-Number"))
		if err != nil {
			w.t.Errorf("part number: %v", err)
		}
		if r.Method != http.MethodPost || (!w.failFirstUploadPart && r.Header.Get("Authorization") != "part-token") {
			w.t.Errorf("upload part method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		w.mu.Lock()
		w.uploads = append(w.uploads, composeUpload{number: number, sha1: r.Header.Get("X-Bz-Content-Sha1"), size: len(body)})
		w.calls = append(w.calls, fmt.Sprintf("upload:%d", number))
		uploadAttempt := len(w.uploads)
		urlCalls := w.uploadURLCalls
		w.mu.Unlock()
		if w.failFirstUploadPart && r.Header.Get("Authorization") != fmt.Sprintf("part-token-%d", urlCalls) {
			w.t.Errorf("upload used stale token %q after %d URL requests", r.Header.Get("Authorization"), urlCalls)
		}
		if w.failFirstUploadPart && uploadAttempt == 1 {
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprint(rw, `{"status":503,"code":"service_unavailable","message":"retry with a new upload URL"}`)
			return
		}
		if w.failUploadPart {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(rw, `{"status":400,"code":"bad_request","message":"upload part failed"}`)
			return
		}
		_, _ = fmt.Fprint(rw, `{}`)
	case "/b2api/v4/b2_copy_part":
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.t.Errorf("decode copy part: %v", err)
		}
		partNumber := int(body["partNumber"].(float64))
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "t" {
			w.t.Errorf("copy part method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		w.mu.Lock()
		w.copies = append(w.copies, body)
		w.calls = append(w.calls, fmt.Sprintf("copy:%d", partNumber))
		fail := w.failCopyPart == partNumber
		w.mu.Unlock()
		if fail {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(rw, `{"status":400,"code":"bad_request","message":"copy part failed"}`)
			return
		}
		size := int64(3)
		if partNumber == 1 {
			size = composeMinPartSize
		}
		if w.copySizeOverride != 0 {
			size = w.copySizeOverride
		}
		_, _ = fmt.Fprintf(rw, `{"fileId":"large","partNumber":%d,"contentLength":%d,"contentSha1":%q}`, partNumber, size, fmt.Sprintf("copy-sha-%d", partNumber))
	case "/b2api/v4/b2_finish_large_file":
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.t.Errorf("decode finish: %v", err)
		}
		w.mu.Lock()
		w.finishes = append(w.finishes, body)
		w.calls = append(w.calls, "finish")
		w.mu.Unlock()
		if w.failFinish {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(rw, `{"status":400,"code":"bad_request","message":"finish failed"}`)
			return
		}
		_, _ = fmt.Fprint(rw, `{"fileId":"large","fileName":"composed","action":"upload"}`)
	case "/b2api/v4/b2_cancel_large_file":
		w.addCall("cancel")
		if w.failCancel {
			rw.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(rw, `{"status":400,"code":"bad_request","message":"cancel failed"}`)
			return
		}
		_, _ = fmt.Fprint(rw, `{}`)
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(rw, r)
	}
}

func (w *composeWire) addCall(call string) {
	w.mu.Lock()
	w.calls = append(w.calls, call)
	w.mu.Unlock()
}

func (w *composeWire) bucket(t *testing.T) *Bucket {
	t.Helper()
	client, err := NewClient(context.Background(), "account", "key", APIBase(w.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func (w *composeWire) snapshot() (calls []string, copies, finishes []map[string]any, uploads []composeUpload) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...), append([]map[string]any(nil), w.copies...), append([]map[string]any(nil), w.finishes...), append([]composeUpload(nil), w.uploads...)
}

func TestComposeValidationRejectsInvalidPartsBeforeStarting(t *testing.T) {
	wire := newComposeWire(t)
	bucket := wire.bucket(t)
	large := bytes.Repeat([]byte("x"), composeMinPartSize)
	tests := []struct {
		name  string
		parts []ComposePart
		want  string
	}{
		{"no parts", nil, "requires at least two"},
		{"one part", []ComposePart{PartFromReader(bytes.NewReader([]byte("x")), 1)}, "requires at least two"},
		{"short uploaded non-final", []ComposePart{PartFromReader(bytes.NewReader([]byte("x")), 1), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "nonfinal part"},
		{"short copied non-final", []ComposePart{PartFromCopy("source", "bytes=0-4999998"), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "nonfinal part"},
		{"non-final copy without range", []ComposePart{PartFromCopy("source", ""), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "needs a byte range"},
		{"reader and source conflict", []ComposePart{{reader: bytes.NewReader(large), size: composeMinPartSize, sourceFileID: "source"}, PartFromReader(bytes.NewReader([]byte("y")), 1)}, "positive size and no copy source"},
		{"reader and copy range conflict", []ComposePart{{reader: bytes.NewReader(large), size: composeMinPartSize, byteRange: "bytes=0-4999999"}, PartFromReader(bytes.NewReader([]byte("y")), 1)}, "copy range"},
		{"copy and upload size conflict", []ComposePart{{sourceFileID: "source", byteRange: "bytes=0-4999999", size: composeMinPartSize}, PartFromReader(bytes.NewReader([]byte("y")), 1)}, "upload size"},
		{"oversized uploaded part", []ComposePart{PartFromReader(bytes.NewReader(nil), 5e9+1), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "exceeds"},
		{"oversized copied range", []ComposePart{PartFromCopy("source", "bytes=0-5000000000"), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "exceeds"},
		{"malformed copy range", []ComposePart{PartFromCopy("source", "bytes=wrong"), PartFromReader(bytes.NewReader([]byte("y")), 1)}, "invalid copy range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := bucket.Compose(context.Background(), "invalid", tc.parts...); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compose error = %v, want %q", err, tc.want)
			}
			calls, _, _, _ := wire.snapshot()
			if len(calls) != 0 {
				t.Fatalf("invalid Compose made calls %v", calls)
			}
		})
	}
}

func TestComposeRejectsMoreThan10000PartsBeforeStarting(t *testing.T) {
	wire := newComposeWire(t)
	wire.failCopyPart = 1 // Bound the unfixed path to one request.
	parts := make([]ComposePart, 10001)
	for i := range parts {
		parts[i] = PartFromCopy(fmt.Sprintf("source-%d", i), "bytes=0-4999999")
	}
	if _, err := wire.bucket(t).Compose(context.Background(), "too-many", parts...); err == nil || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("Compose with 10001 parts error = %v, want preflight rejection", err)
	}
	calls, _, _, _ := wire.snapshot()
	if len(calls) != 0 {
		t.Fatalf("too many parts started an upload: %v", calls)
	}
}

func TestComposeAllSourceMixesPreservePartHashOrder(t *testing.T) {
	first := bytes.Repeat([]byte("x"), composeMinPartSize)
	firstHash := fmt.Sprintf("%x", sha1.Sum(first))
	last := []byte("last")
	lastHash := fmt.Sprintf("%x", sha1.Sum(last))
	for _, tc := range []struct {
		name       string
		parts      []ComposePart
		wantCalls  []string
		wantHashes []any
		wantCopies []map[string]any
	}{
		{
			name:       "mixed",
			parts:      []ComposePart{PartFromReader(bytes.NewReader(first), composeMinPartSize), PartFromCopy("source-id", "bytes=0-2")},
			wantCalls:  []string{"start", "upload:1", "copy:2", "finish"},
			wantHashes: []any{firstHash, "copy-sha-2"},
			wantCopies: []map[string]any{{"sourceFileId": "source-id", "largeFileId": "large", "partNumber": float64(2), "range": "bytes=0-2"}},
		},
		{
			name:       "copy only",
			parts:      []ComposePart{PartFromCopy("source-one", "bytes=0-4999999"), PartFromCopy("source-two", "bytes=1-3")},
			wantCalls:  []string{"start", "copy:1", "copy:2", "finish"},
			wantHashes: []any{"copy-sha-1", "copy-sha-2"},
			wantCopies: []map[string]any{
				{"sourceFileId": "source-one", "largeFileId": "large", "partNumber": float64(1), "range": "bytes=0-4999999"},
				{"sourceFileId": "source-two", "largeFileId": "large", "partNumber": float64(2), "range": "bytes=1-3"},
			},
		},
		{
			name:       "adjacent copy ranges",
			parts:      []ComposePart{PartFromCopy("same-source", "bytes=0-4999999"), PartFromCopy("same-source", "bytes=5000000-5000002")},
			wantCalls:  []string{"start", "copy:1", "copy:2", "finish"},
			wantHashes: []any{"copy-sha-1", "copy-sha-2"},
			wantCopies: []map[string]any{
				{"sourceFileId": "same-source", "largeFileId": "large", "partNumber": float64(1), "range": "bytes=0-4999999"},
				{"sourceFileId": "same-source", "largeFileId": "large", "partNumber": float64(2), "range": "bytes=5000000-5000002"},
			},
		},
		{
			name:       "whole source as final part",
			parts:      []ComposePart{PartFromCopy("source-one", "bytes=0-4999999"), PartFromCopy("source-two", "")},
			wantCalls:  []string{"start", "copy:1", "copy:2", "finish"},
			wantHashes: []any{"copy-sha-1", "copy-sha-2"},
			wantCopies: []map[string]any{
				{"sourceFileId": "source-one", "largeFileId": "large", "partNumber": float64(1), "range": "bytes=0-4999999"},
				{"sourceFileId": "source-two", "largeFileId": "large", "partNumber": float64(2)},
			},
		},
		{
			name:       "upload only",
			parts:      []ComposePart{PartFromReader(bytes.NewReader(first), composeMinPartSize), PartFromReader(bytes.NewReader(last), int64(len(last)))},
			wantCalls:  []string{"start", "upload:1", "upload:2", "finish"},
			wantHashes: []any{firstHash, lastHash},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newComposeWire(t)
			object, err := wire.bucket(t).Compose(context.Background(), "composed", tc.parts...)
			if err != nil {
				t.Fatal(err)
			}
			if object.ID() != "large" {
				t.Errorf("object ID = %q, want large", object.ID())
			}
			wire.mu.Lock()
			starts := append([]map[string]any(nil), wire.starts...)
			wire.mu.Unlock()
			if len(starts) != 1 || starts[0]["fileName"] != "composed" || starts[0]["contentType"] != "application/octet-stream" {
				t.Errorf("start bodies = %#v, want composed file and content type", starts)
			}
			calls, copies, finishes, uploads := wire.snapshot()
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, tc.wantCalls)
			}
			if !reflect.DeepEqual(copies, tc.wantCopies) {
				t.Errorf("copy bodies = %#v, want %#v", copies, tc.wantCopies)
			}
			if len(finishes) != 1 || !reflect.DeepEqual(finishes[0]["partSha1Array"], tc.wantHashes) {
				t.Errorf("finish bodies = %#v, want partSha1Array %#v", finishes, tc.wantHashes)
			}
			for _, upload := range uploads {
				if upload.sha1 == "" || upload.size == 0 {
					t.Errorf("upload %#v did not carry its sha1 and bytes", upload)
				}
			}
		})
	}
}

func TestComposeRejectsOverlappingRangesBeforeStarting(t *testing.T) {
	wire := newComposeWire(t)
	_, err := wire.bucket(t).Compose(context.Background(), "overlap",
		PartFromCopy("same-source", "bytes=0-4999999"),
		PartFromCopy("same-source", "bytes=4999999-5000001"))
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping copy ranges error = %v, want rejection", err)
	}
	calls, _, _, _ := wire.snapshot()
	if len(calls) != 0 {
		t.Fatalf("overlap started a large file: %v", calls)
	}
}

func TestComposeRetriesRecoverableUploadWithFreshURL(t *testing.T) {
	wire := newComposeWire(t)
	wire.failFirstUploadPart = true
	payload := bytes.Repeat([]byte("r"), composeMinPartSize)
	object, err := wire.bucket(t).Compose(context.Background(), "retry",
		PartFromReader(bytes.NewReader(payload), int64(len(payload))),
		PartFromCopy("source", "bytes=0-2"))
	if err != nil {
		t.Fatal(err)
	}
	if object.ID() != "large" {
		t.Fatalf("composed ID = %q, want large", object.ID())
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if wire.uploadURLCalls != 2 || len(wire.uploads) != 2 {
		t.Fatalf("upload URL calls = %d, upload attempts = %d; want 2 each", wire.uploadURLCalls, len(wire.uploads))
	}
	if !reflect.DeepEqual(wire.uploads[0], wire.uploads[1]) {
		t.Errorf("retry did not replay the same buffered part: %#v", wire.uploads)
	}
	if want := []string{"start", "upload:1", "upload:1", "copy:2", "finish"}; !reflect.DeepEqual(wire.calls, want) {
		t.Errorf("calls = %v, want %v", wire.calls, want)
	}
}

type composeCancelReader struct{ cancel context.CancelFunc }

func (r composeCancelReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, fmt.Errorf("injected read failure")
}

type composeExpiringContext struct {
	context.Context
	done    chan struct{}
	expired atomic.Bool
}

func (c *composeExpiringContext) Deadline() (time.Time, bool) {
	if c.expired.Load() {
		return time.Now().Add(-time.Second), true
	}
	return time.Now().Add(time.Hour), true
}

func (c *composeExpiringContext) Done() <-chan struct{} { return c.done }

func (c *composeExpiringContext) Err() error {
	if c.expired.Load() {
		return context.DeadlineExceeded
	}
	return nil
}

type composeDeadlineReader struct{ ctx *composeExpiringContext }

func (r composeDeadlineReader) Read([]byte) (int, error) {
	r.ctx.expired.Store(true)
	close(r.ctx.done)
	return 0, context.DeadlineExceeded
}

func TestComposeCancelsAfterCallerDeadlineExpires(t *testing.T) {
	wire := newComposeWire(t)
	ctx := &composeExpiringContext{Context: context.Background(), done: make(chan struct{})}
	_, err := wire.bucket(t).Compose(ctx, "deadline",
		PartFromCopy("source", "bytes=0-4999999"),
		PartFromReader(composeDeadlineReader{ctx: ctx}, 1))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Compose error = %v, want deadline exceeded", err)
	}
	calls, _, finishes, _ := wire.snapshot()
	if want := []string{"start", "copy:1", "cancel"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls after deadline = %v, want %v", calls, want)
	}
	if len(finishes) != 0 {
		t.Fatalf("finished after deadline: %#v", finishes)
	}
}

func TestComposeCancelsAfterCallerContextIsCanceled(t *testing.T) {
	wire := newComposeWire(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := wire.bucket(t).Compose(ctx, "canceled",
		PartFromCopy("source", "bytes=0-4999999"),
		PartFromReader(composeCancelReader{cancel: cancel}, 1))
	if err == nil || !strings.Contains(err.Error(), "injected read failure") {
		t.Fatalf("Compose error = %v, want read failure", err)
	}
	calls, _, finishes, _ := wire.snapshot()
	if want := []string{"start", "copy:1", "cancel"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls after cancellation = %v, want %v", calls, want)
	}
	if len(finishes) != 0 {
		t.Fatalf("finished after failure: %#v", finishes)
	}
}

func TestComposeRejectsShortCopyResponseAndCancels(t *testing.T) {
	wire := newComposeWire(t)
	wire.copySizeOverride = composeMinPartSize - 1
	_, err := wire.bucket(t).Compose(context.Background(), "short-copy",
		PartFromCopy("source", "bytes=0-4999999"),
		PartFromReader(bytes.NewReader([]byte("x")), 1))
	if err == nil || !strings.Contains(err.Error(), "copied") {
		t.Fatalf("short copy response error = %v, want rejection", err)
	}
	calls, _, finishes, _ := wire.snapshot()
	if want := []string{"start", "copy:1", "cancel"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("short copy calls = %v, want %v", calls, want)
	}
	if len(finishes) != 0 {
		t.Fatalf("finished short copy: %#v", finishes)
	}
}

func TestComposeReportsCancelFailureAlongsidePartFailure(t *testing.T) {
	wire := newComposeWire(t)
	wire.failCopyPart = 2
	wire.failCancel = true
	_, err := wire.bucket(t).Compose(context.Background(), "cancel-fails",
		PartFromCopy("source", "bytes=0-4999999"),
		PartFromCopy("source-two", "bytes=0-2"))
	if err == nil || !strings.Contains(err.Error(), "copy part failed") || !strings.Contains(err.Error(), "cancel failed") {
		t.Fatalf("Compose error = %v, want both part and cleanup errors", err)
	}
}

func TestComposeCancelsLargeFileAfterPartFailure(t *testing.T) {
	wire := newComposeWire(t)
	wire.failCopyPart = 2
	_, err := wire.bucket(t).Compose(context.Background(), "failed",
		PartFromCopy("source-one", "bytes=0-4999999"),
		PartFromCopy("source-two", "bytes=0-2"))
	if err == nil || !strings.Contains(err.Error(), "copy part failed") {
		t.Fatalf("Compose error = %v, want the copy-part failure", err)
	}
	calls, _, finishes, _ := wire.snapshot()
	if want := []string{"start", "copy:1", "copy:2", "cancel"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	if len(finishes) != 0 {
		t.Errorf("finish called after part failure: %#v", finishes)
	}
}

func TestComposeCancelsOnUploadAndFinishFailures(t *testing.T) {
	for _, tc := range []struct {
		name, wantErr string
		configure     func(*composeWire)
		parts         []ComposePart
		wantCalls     []string
	}{
		{"upload", "upload part failed", func(w *composeWire) { w.failUploadPart = true }, []ComposePart{PartFromCopy("source", "bytes=0-4999999"), PartFromReader(bytes.NewReader([]byte("x")), 1)}, []string{"start", "copy:1", "upload:2", "cancel"}},
		{"finish", "finish failed", func(w *composeWire) { w.failFinish = true }, []ComposePart{PartFromCopy("source-one", "bytes=0-4999999"), PartFromCopy("source-two", "bytes=0-2")}, []string{"start", "copy:1", "copy:2", "finish", "cancel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newComposeWire(t)
			tc.configure(wire)
			if _, err := wire.bucket(t).Compose(context.Background(), "failure", tc.parts...); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Compose error = %v, want %q", err, tc.wantErr)
			}
			calls, _, _, _ := wire.snapshot()
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}
