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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type sseB2WireRequest struct {
	method  string
	path    string
	headers http.Header
	body    map[string]any
}

type sseB2Wire struct {
	t *testing.T

	srv *httptest.Server
	mu  sync.Mutex

	requests []sseB2WireRequest
	objects  map[string][]byte
}

func newSSEB2Wire(t *testing.T) *sseB2Wire {
	t.Helper()
	w := &sseB2Wire{t: t, objects: make(map[string][]byte)}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serveHTTP))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *sseB2Wire) serveHTTP(rw http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/b2_authorize_account") {
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
		return
	}
	recorded := sseB2WireRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone()}
	if strings.HasPrefix(r.URL.Path, "/b2api/") {
		recorded.body = map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&recorded.body); err != nil {
			w.t.Errorf("decode %s: %v", r.URL.Path, err)
		}
	}
	w.mu.Lock()
	w.requests = append(w.requests, recorded)
	w.mu.Unlock()

	switch {
	case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
		_, _ = fmt.Fprint(rw, `{"buckets":[]}`)
	case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
		_, _ = fmt.Fprint(rw, noReplicationBucketJSON)
	case strings.HasSuffix(r.URL.Path, "/b2_get_upload_url"):
		_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":"upload-token"}`, w.srv.URL+"/upload")
	case r.URL.Path == "/upload":
		data, err := io.ReadAll(r.Body)
		if err != nil {
			w.t.Error(err)
		}
		name := r.Header.Get("X-Bz-File-Name")
		w.mu.Lock()
		w.objects[name] = data
		w.mu.Unlock()
		_, _ = fmt.Fprintf(rw, `{"fileId":%q,"fileName":%q,"action":"upload","contentLength":%d,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`, "id-"+name, name, len(data))
	case strings.HasSuffix(r.URL.Path, "/b2_start_large_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"large"}`)
	case strings.HasSuffix(r.URL.Path, "/b2_get_upload_part_url"):
		_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":"part-token"}`, w.srv.URL+"/part")
	case r.URL.Path == "/part":
		_, _ = fmt.Fprint(rw, `{}`)
	case strings.HasSuffix(r.URL.Path, "/b2_finish_large_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"large","fileName":"large","action":"upload"}`)
	case strings.HasPrefix(r.URL.Path, "/file/bucket/"):
		w.serveDownload(rw, r)
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(rw, r)
	}
}

func (w *sseB2Wire) serveDownload(rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/file/bucket/")
	w.mu.Lock()
	data := append([]byte(nil), w.objects[name]...)
	w.mu.Unlock()
	if r.Header.Get("Range") != "" && !strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
		rw.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		_, _ = fmt.Fprint(rw, `{"status":416,"code":"range_not_satisfiable","message":"past end"}`)
		return
	}
	rw.Header().Set("Content-Length", fmt.Sprint(len(data)))
	rw.Header().Set("Content-Type", "application/octet-stream")
	rw.Header().Set("X-Bz-File-Id", "id-"+name)
	rw.Header().Set("X-Bz-Content-Sha1", "0000000000000000000000000000000000000000")
	if r.Method != http.MethodHead {
		_, _ = rw.Write(data)
	}
}

func (w *sseB2Wire) bucket(t *testing.T) *Bucket {
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

func (w *sseB2Wire) requestsTo(path string) []sseB2WireRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	var result []sseB2WireRequest
	for _, request := range w.requests {
		if request.path == path {
			result = append(result, request)
		}
	}
	return result
}

func assertSSEB2(t *testing.T, body map[string]any) {
	t.Helper()
	got, ok := body["serverSideEncryption"].(map[string]any)
	if !ok || got["mode"] != "SSE-B2" || got["algorithm"] != "AES256" {
		t.Errorf("serverSideEncryption = %#v, want SSE-B2/AES256", body["serverSideEncryption"])
	}
}

func TestWithSSEB2WireAndRoundTrip(t *testing.T) {
	wire := newSSEB2Wire(t)
	bucket := wire.bucket(t)
	ctx := context.Background()
	payload := []byte("per-request encryption")

	object := bucket.Object("encrypted")
	writer := object.NewWriter(ctx, WithSSEB2("AES256"))
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploads := wire.requestsTo("/upload")
	if len(uploads) != 1 || uploads[0].method != http.MethodPost || uploads[0].headers.Get("Authorization") != "upload-token" {
		t.Fatalf("upload requests = %#v, want one authenticated POST", uploads)
	}
	if got := uploads[0].headers.Get("X-Bz-Server-Side-Encryption"); got != "AES256" {
		t.Errorf("upload SSE-B2 header = %q, want AES256", got)
	}

	reader := object.NewReader(ctx)
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("round-trip bytes = %q, want %q", got, payload)
	}

	plain := bucket.Object("plain").NewWriter(ctx)
	if _, err := plain.Write([]byte("plain")); err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	uploads = wire.requestsTo("/upload")
	if len(uploads) != 2 {
		t.Fatalf("upload requests = %d, want 2", len(uploads))
	}
	if got := uploads[1].headers.Get("X-Bz-Server-Side-Encryption"); got != "" {
		t.Errorf("upload without option sent SSE-B2 header %q", got)
	}

	large := bucket.Object("large").NewWriter(ctx, WithSSEB2("AES256"))
	large.ChunkSize = 1
	if _, err := large.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	starts := wire.requestsTo("/b2api/v4/b2_start_large_file")
	if len(starts) != 1 {
		t.Fatalf("start_large_file requests = %d, want 1", len(starts))
	}
	assertSSEB2(t, starts[0].body)
	parts := wire.requestsTo("/part")
	if len(parts) != 1 || parts[0].headers.Get("X-Bz-Server-Side-Encryption") != "" {
		t.Errorf("upload_part requests = %#v, want one request without a per-part SSE-B2 header", parts)
	}
}

func TestWithSSEB2RejectsInvalidAlgorithmsWithoutUpload(t *testing.T) {
	for _, algorithm := range []string{"", "AES128"} {
		t.Run(fmt.Sprintf("%q", algorithm), func(t *testing.T) {
			wire := newSSEB2Wire(t)
			writer := wire.bucket(t).Object("invalid").NewWriter(context.Background(), WithSSEB2(algorithm))
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err == nil || !strings.Contains(err.Error(), "AES256") {
				t.Fatalf("Close error = %v, want an AES256 validation error", err)
			}
			if uploads := wire.requestsTo("/upload"); len(uploads) != 0 {
				t.Errorf("invalid algorithm sent %d upload request(s)", len(uploads))
			}
			if starts := wire.requestsTo("/b2api/v4/b2_start_large_file"); len(starts) != 0 {
				t.Errorf("invalid algorithm sent %d start_large_file request(s)", len(starts))
			}
		})
	}
}
