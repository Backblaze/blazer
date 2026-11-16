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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

type progressWireRequest struct {
	method, path, auth, name, byteRange string
}

type progressWire struct {
	server       *httptest.Server
	mu           sync.Mutex
	objects      map[string][]byte
	requests     []progressWireRequest
	uploadSeen   <-chan struct{}
	downloadSeen <-chan struct{}
}

func newProgressWire(t *testing.T) (*progressWire, *Bucket) {
	t.Helper()
	w := &progressWire{objects: make(map[string][]byte)}
	w.server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		w.requests = append(w.requests, progressWireRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Bz-File-Name"), r.Header.Get("Range")})
		w.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(rw, updateAuthJSON, w.server.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprint(rw, `{"buckets":[]}`)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			_, _ = fmt.Fprint(rw, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_get_upload_url"):
			_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":"upload-token"}`, w.server.URL+"/upload")
		case r.URL.Path == "/upload":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read upload: %v", err)
				return
			}
			if r.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" {
				if len(data) < 40 {
					t.Errorf("missing upload SHA1 trailer")
					return
				}
				data = data[:len(data)-40]
			}
			name := r.Header.Get("X-Bz-File-Name")
			w.mu.Lock()
			w.objects[name] = append([]byte(nil), data...)
			w.mu.Unlock()
			_, _ = fmt.Fprintf(rw, `{"fileId":%q,"fileName":%q,"action":"upload","contentLength":%d,"contentSha1":"none","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`, "id-"+name, name, len(data))
		case strings.HasPrefix(r.URL.Path, "/file/n/"):
			w.download(t, rw, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(w.server.Close)
	transport := &progressWireTransport{wire: w, next: http.DefaultTransport}
	client, err := NewClient(context.Background(), "account", "key", APIBase(w.server.URL), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(context.Background(), "n", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return w, bucket
}

func (w *progressWire) download(t *testing.T, rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/file/n/")
	w.mu.Lock()
	data := append([]byte(nil), w.objects[name]...)
	w.mu.Unlock()
	if data == nil {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("X-Bz-File-Id", "id-"+name)
	if r.Method == http.MethodHead {
		rw.Header().Set("Content-Length", strconv.Itoa(len(data)))
		return
	}
	response := b2wiretest.RangeDownload(string(data), r.Header.Get("Range"))
	for name, values := range response.Header {
		rw.Header()[name] = values
	}
	if response.Status >= http.StatusBadRequest {
		rw.WriteHeader(response.Status)
		_, _ = fmt.Fprint(rw, response.Body)
		return
	}
	chunk := []byte(response.Body)
	rw.WriteHeader(response.Status)
	first := len(chunk)
	if first > 1024 {
		first = 1024
	}
	_, _ = rw.Write(chunk[:first])
	if first < len(chunk) && w.downloadSeen != nil {
		rw.(http.Flusher).Flush()
		select {
		case <-w.downloadSeen:
		case <-r.Context().Done():
			return
		}
	}
	_, _ = rw.Write(chunk[first:])
}

type progressWireTransport struct {
	wire *progressWire
	next http.RoundTripper
}

func (t *progressWireTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/upload" && t.wire.uploadSeen != nil {
		copy := r.Clone(r.Context())
		copy.Body = &progressGateBody{ReadCloser: r.Body, seen: t.wire.uploadSeen, ctx: r.Context()}
		return t.next.RoundTrip(copy)
	}
	return t.next.RoundTrip(r)
}

type progressGateBody struct {
	io.ReadCloser
	seen  <-chan struct{}
	ctx   context.Context
	first bool
}

func (b *progressGateBody) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true
		if len(p) > 1024 {
			p = p[:1024]
		}
		return b.ReadCloser.Read(p)
	}
	select {
	case <-b.seen:
		return b.ReadCloser.Read(p)
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}

func (w *progressWire) requestsFor(path string) []progressWireRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []progressWireRequest
	for _, r := range w.requests {
		if r.path == path {
			out = append(out, r)
		}
	}
	return out
}

// A genuine NewClient drives the HTTP upload and download paths. The gates
// require an intermediate callback before either body can finish transferring.
func TestProgressNewClientWire(t *testing.T) {
	wire, bucket := newProgressWire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload := bytes.Repeat([]byte("progress-wire"), 10_000)

	for _, tc := range []struct {
		name      string
		known     bool
		wantTotal int64
	}{
		{"unknown", false, -1},
		{"known", true, int64(len(payload))},
	} {
		t.Run("upload "+tc.name, func(t *testing.T) {
			seen := make(chan struct{})
			wire.uploadSeen = seen
			var once sync.Once
			var mu sync.Mutex
			var samples []progressSample
			writer := bucket.Object(tc.name).NewWriter(ctx, WithProgress(func(complete, total int64) {
				mu.Lock()
				samples = append(samples, progressSample{complete, total})
				mu.Unlock()
				if complete > 0 && complete < int64(len(payload)) {
					once.Do(func() { close(seen) })
				}
			}))
			if tc.known {
				if n, err := writer.ReadFrom(bytes.NewReader(payload)); err != nil || n != int64(len(payload)) {
					t.Fatalf("ReadFrom = (%d, %v)", n, err)
				}
			} else if n, err := writer.Write(payload); err != nil || n != len(payload) {
				t.Fatalf("Write = (%d, %v)", n, err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), tc.wantTotal, true)
			requests := wire.requestsFor("/upload")
			if len(requests) == 0 {
				t.Fatal("no upload request reached the server")
			}
			if got := requests[len(requests)-1]; got.method != http.MethodPost || got.auth != "upload-token" || got.name != tc.name {
				t.Fatalf("upload wire request = %+v", got)
			}
			wire.mu.Lock()
			got := append([]byte(nil), wire.objects[tc.name]...)
			wire.mu.Unlock()
			if !bytes.Equal(got, payload) {
				t.Fatal("uploaded bytes differ")
			}
		})
	}
	wire.uploadSeen = nil

	for _, tc := range []struct {
		name      string
		ranged    bool
		wantTotal int64
	}{
		{"range", true, int64(len(payload) - 2048)},
		{"full", false, -1},
	} {
		t.Run("download "+tc.name, func(t *testing.T) {
			seen := make(chan struct{})
			wire.downloadSeen = seen
			var once sync.Once
			var mu sync.Mutex
			var samples []progressSample
			var reader *Reader
			if tc.ranged {
				reader = bucket.Object("unknown").NewRangeReader(ctx, 2048, int64(len(payload)))
			} else {
				reader = bucket.Object("unknown").NewReader(ctx)
			}
			reader.Progress = func(complete, total int64) {
				mu.Lock()
				samples = append(samples, progressSample{complete, total})
				mu.Unlock()
				if complete > 0 && complete < int64(len(payload)-2048) {
					once.Do(func() { close(seen) })
				}
			}
			got, err := io.ReadAll(reader)
			if closeErr := reader.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			want := payload
			if tc.ranged {
				want = payload[2048:]
			}
			if !bytes.Equal(got, want) {
				t.Fatal("downloaded bytes differ")
			}
			assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(want)), tc.wantTotal, true)
			requests := wire.requestsFor("/file/n/unknown")
			if len(requests) == 0 || requests[len(requests)-1].auth != "t" {
				t.Fatalf("download wire requests = %+v", requests)
			}
			if tc.ranged {
				found := false
				for _, r := range requests {
					if r.method == http.MethodGet && r.byteRange == fmt.Sprintf("bytes=2048-%d", len(payload)+2047) {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing requested range in %+v", requests)
				}
			}
		})
	}
}

// Consuming the request body does not mean B2 accepted the upload.
func TestProgressRejectedSimpleUploadWire(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.StrictReplySequence("upload_file", b2wiretest.Response{
		Status: http.StatusBadRequest,
		Body:   `{"status":400,"code":"bad_request","message":"rejected after body"}`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var samples []progressSample
	writer := bucket.Object("progress-rejected").NewWriter(ctx, WithProgress(func(complete, total int64) {
		samples = append(samples, progressSample{complete, total})
	}))
	if n, err := writer.ReadFrom(bytes.NewReader([]byte("abc"))); n != 3 || err != nil {
		t.Fatalf("ReadFrom = %d, %v; want 3, nil", n, err)
	}
	err := writer.Close()
	if status, code, message := base.MsgCode(err); status != 400 || code != "bad_request" || message != "rejected after body" {
		t.Fatalf("Close = %v; want scripted 400/bad_request/rejected after body", err)
	}
	requests := srv.Calls("upload_file")
	if len(requests) != 1 {
		t.Fatalf("upload requests = %d, want 1", len(requests))
	}
	assertProgressUploadBody(t, requests[0], []byte("abc"))
	assertProgressSamples(t, samples, 3, 3, false)
}

func TestProgressExhaustedMultipartUploadWire(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.StrictReplySequence("b2_start_large_file", b2wiretest.Response{
		Body: `{"fileId":"progress-large","fileName":"progress-exhausted","action":"start","fileInfo":{},"contentType":"application/octet-stream","uploadTimestamp":1}`,
	})
	const partSize = 5_000_000 // B2's minimum non-final part size.
	part := bytes.Repeat([]byte("a"), partSize)
	responses := []b2wiretest.Response{
		{Body: fmt.Sprintf(`{"fileId":"progress-large","partNumber":1,"contentLength":5000000,"contentSha1":"%x","uploadTimestamp":1}`, sha1.Sum(part))},
		{Body: fmt.Sprintf(`{"fileId":"progress-large","partNumber":2,"contentLength":5000000,"contentSha1":"%x","uploadTimestamp":1}`, sha1.Sum(part))},
	}
	for range 6 { // Initial attempt plus the existing five reuploads.
		responses = append(responses, b2wiretest.Response{
			Status: http.StatusInternalServerError,
			Body:   `{"status":500,"code":"internal_error","message":"part rejected after body"}`,
		})
	}
	srv.StrictReplySequence("upload_part", responses...)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"progress-large","fileName":"progress-exhausted","action":"upload"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload := append(append(append([]byte(nil), part...), part...), []byte("end")...)
	var samples []progressSample
	writer := bucket.Object("progress-exhausted").NewWriter(ctx, WithProgress(func(complete, total int64) {
		samples = append(samples, progressSample{complete, total})
	}))
	writer.ChunkSize = partSize
	writer.ConcurrentUploads = 1 // Make the final part the only failing part.
	if n, err := writer.ReadFrom(bytes.NewReader(payload)); n != 10_000_003 || err != nil {
		t.Fatalf("ReadFrom = %d, %v; want 10000003, nil", n, err)
	}
	err := writer.Close()
	if status, code, message := base.MsgCode(err); status != 500 || code != "internal_error" || message != "part rejected after body" {
		t.Fatalf("Close = %v; want scripted 500/internal_error/part rejected after body", err)
	}
	requests := srv.Calls("upload_part")
	if len(requests) != 8 {
		t.Fatalf("part requests = %d, want 2 successful parts and 6 failed attempts", len(requests))
	}
	for i, request := range requests {
		wantPart, wantBody := "3", []byte("end")
		if i < 2 {
			wantPart, wantBody = strconv.Itoa(i+1), part
		}
		if got := request.Header.Get("X-Bz-Part-Number"); got != wantPart {
			t.Fatalf("request %d part = %q, want %q", i, got, wantPart)
		}
		assertProgressUploadBody(t, request, wantBody)
	}
	if got := len(srv.Calls("b2_finish_large_file")); got != 0 {
		t.Fatalf("finish requests = %d, want 0 after a failed part", got)
	}
	// Re-reading the final three bytes six times must not count them six times.
	assertProgressSamples(t, samples, 10_000_003, 10_000_003, false)
}

func assertProgressUploadBody(t *testing.T, request b2wiretest.Request, payload []byte) {
	t.Helper()
	want := append(append([]byte(nil), payload...), []byte(fmt.Sprintf("%x", sha1.Sum(payload)))...)
	if request.Header.Get("X-Bz-Content-Sha1") != "hex_digits_at_end" || !bytes.Equal(request.Body, want) {
		t.Fatalf("upload body or SHA1 trailer differs: got %d bytes, want %d", len(request.Body), len(want))
	}
}

func TestProgressRangeAtAndPastEOFWire(t *testing.T) {
	for _, offset := range []int64{3, 4} {
		t.Run(fmt.Sprintf("offset=%d", offset), func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Handle("progress-eof", func(request b2wiretest.Request) b2wiretest.Response {
				if request.Method == http.MethodHead {
					return b2wiretest.Response{Header: http.Header{"Content-Length": {"3"}}}
				}
				return b2wiretest.RangeDownload("abc", request.Header.Get("Range"))
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var samples []progressSample
			reader := bucket.Object("progress-eof").NewRangeReader(ctx, offset, 5)
			reader.Progress = func(complete, total int64) { samples = append(samples, progressSample{complete, total}) }
			got, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if len(got) != 0 || readErr != nil || closeErr != nil {
				t.Fatalf("ReadAll = %q, %v; Close = %v; want empty, nil, nil", got, readErr, closeErr)
			}
			assertProgressSamples(t, samples, 0, 0, false)
			requests := srv.Calls("progress-eof")
			if len(requests) != 1 || requests[0].Method != http.MethodHead {
				t.Fatalf("download requests = %+v, want one HEAD and no GET", requests)
			}
		})
	}
}

func TestProgressZeroAndNegativeRangeLengthWire(t *testing.T) {
	for _, withProgress := range []bool{false, true} {
		for _, length := range []int64{0, -1} {
			t.Run(fmt.Sprintf("progress=%t/length=%d", withProgress, length), func(t *testing.T) {
				srv, bucket := newSHA1WireBucket(t)
				srv.Handle("progress-length", func(request b2wiretest.Request) b2wiretest.Response {
					return b2wiretest.RangeDownload("abc", request.Header.Get("Range"))
				})
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				reader := bucket.Object("progress-length").NewRangeReader(ctx, 1, length)
				reader.ChunkSize = 8
				var samples []progressSample
				if withProgress {
					reader.Progress = func(complete, total int64) { samples = append(samples, progressSample{complete, total}) }
				}
				got, readErr := io.ReadAll(reader)
				closeErr := reader.Close()
				want, total := "", int64(0)
				if length < 0 {
					want, total = "bc", -1
				}
				if string(got) != want || readErr != nil || closeErr != nil {
					t.Fatalf("ReadAll = %q, %v; Close = %v; want %q, nil, nil", got, readErr, closeErr, want)
				}
				if withProgress {
					assertProgressSamples(t, samples, int64(len(want)), total, false)
				}
				requests := srv.Calls("progress-length")
				if (length == 0 && len(requests) != 0) || (length < 0 && len(requests) == 0) {
					t.Fatalf("download requests = %+v; want none for zero length and GETs for negative length", requests)
				}
				for _, request := range requests {
					if request.Method != http.MethodGet {
						t.Fatalf("open-ended read made a %s request, want only GET", request.Method)
					}
				}
			})
		}
	}
}
