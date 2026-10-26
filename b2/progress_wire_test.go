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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
	start, end := int64(0), int64(len(data)-1)
	if h := r.Header.Get("Range"); h != "" {
		bounds := strings.SplitN(strings.TrimPrefix(h, "bytes="), "-", 2)
		if len(bounds) != 2 {
			t.Errorf("invalid Range %q", h)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		var err error
		start, err = strconv.ParseInt(bounds[0], 10, 64)
		if err != nil {
			t.Errorf("invalid Range %q: %v", h, err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		if bounds[1] != "" {
			end, err = strconv.ParseInt(bounds[1], 10, 64)
			if err != nil {
				t.Errorf("invalid Range %q: %v", h, err)
				rw.WriteHeader(http.StatusBadRequest)
				return
			}
		}
	}
	if start < 0 || start >= int64(len(data)) {
		rw.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		_, _ = fmt.Fprint(rw, `{"status":416,"code":"range_not_satisfiable","message":"past end"}`)
		return
	}
	if end >= int64(len(data)) {
		end = int64(len(data)) - 1
	}
	if end < start {
		t.Errorf("invalid download range: %d-%d", start, end)
		rw.WriteHeader(http.StatusBadRequest)
		return
	}
	chunk := data[int(start) : int(end)+1]
	rw.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
	if r.Header.Get("Range") != "" {
		rw.WriteHeader(http.StatusPartialContent)
	}
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
