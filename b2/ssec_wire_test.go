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
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type ssecPublicRequest struct {
	method  string
	path    string
	headers http.Header
	body    map[string]any
}

type ssecPublicWire struct {
	t        *testing.T
	srv      *httptest.Server
	key      *SSECKey
	keyValue string
	keyMD5   string

	mu       sync.Mutex
	requests []ssecPublicRequest
	objects  map[string][]byte
}

func newSSECPublicWire(t *testing.T) *ssecPublicWire {
	t.Helper()
	raw := []byte("0123456789abcdef0123456789abcdef")
	key, err := NewSSECKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(raw)
	w := &ssecPublicWire{
		t:        t,
		key:      key,
		keyValue: base64.StdEncoding.EncodeToString(raw),
		keyMD5:   base64.StdEncoding.EncodeToString(sum[:]),
		objects:  make(map[string][]byte),
	}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serveHTTP))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *ssecPublicWire) serveHTTP(rw http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/b2_authorize_account") {
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
		return
	}
	recorded := ssecPublicRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone()}
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
		_, _ = fmt.Fprintf(rw, `{"fileId":%q,"fileName":%q,"action":"upload","contentLength":%d,"contentSha1":"%040d","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`, "id-"+name, name, len(data), 0)
	case strings.HasPrefix(r.URL.Path, "/file/bucket/"):
		w.serveDownload(rw, r)
	case strings.HasSuffix(r.URL.Path, "/b2_get_file_info"):
		_, _ = fmt.Fprint(rw, `{"fileId":"id-encrypted","fileName":"encrypted","action":"upload","contentLength":7,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`)
	case strings.HasSuffix(r.URL.Path, "/b2_copy_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"copy-id","fileName":"copy","action":"copy","contentLength":7,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`)
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(rw, r)
	}
}

func (w *ssecPublicWire) serveDownload(rw http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/file/bucket/")
	if name == "encrypted" && !w.hasKey(r.Header) {
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(rw, `{"status":400,"code":"access_denied","message":"SSE-C key required"}`)
		return
	}
	w.mu.Lock()
	data := append([]byte(nil), w.objects[name]...)
	w.mu.Unlock()
	if name == "encrypted" && data == nil {
		data = []byte("payload")
	}
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

func (w *ssecPublicWire) hasKey(headers http.Header) bool {
	return headers.Get("X-Bz-Server-Side-Encryption-Customer-Algorithm") == "AES256" &&
		headers.Get("X-Bz-Server-Side-Encryption-Customer-Key") == w.keyValue &&
		headers.Get("X-Bz-Server-Side-Encryption-Customer-Key-Md5") == w.keyMD5
}

func (w *ssecPublicWire) newBucket(t *testing.T) *Bucket {
	t.Helper()
	c, err := NewClient(context.Background(), "account", "key", APIBase(w.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (w *ssecPublicWire) requestsTo(path string) []ssecPublicRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	var requests []ssecPublicRequest
	for _, request := range w.requests {
		if request.path == path {
			requests = append(requests, request)
		}
	}
	return requests
}

func assertSSECWireHeaders(t *testing.T, headers http.Header, key, digest string) {
	t.Helper()
	for name, want := range map[string]string{
		"X-Bz-Server-Side-Encryption-Customer-Algorithm": "AES256",
		"X-Bz-Server-Side-Encryption-Customer-Key":       key,
		"X-Bz-Server-Side-Encryption-Customer-Key-Md5":   digest,
	} {
		if got := headers.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func assertNoSSECWireHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	for _, name := range []string{
		"X-Bz-Server-Side-Encryption-Customer-Algorithm",
		"X-Bz-Server-Side-Encryption-Customer-Key",
		"X-Bz-Server-Side-Encryption-Customer-Key-Md5",
	} {
		if got := headers.Get(name); got != "" {
			t.Errorf("unexpected %s = %q", name, got)
		}
	}
}

func TestSSECWriterDownloadAttrsAndCopyWire(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	ctx := context.Background()
	payload := []byte("payload")

	encrypted := bucket.Object("encrypted")
	writer := encrypted.NewWriter(ctx, WithSSECKey(wire.key))
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploads := wire.requestsTo("/upload")
	if len(uploads) != 1 || uploads[0].headers.Get("Authorization") != "upload-token" {
		t.Fatalf("upload requests = %#v, want one request with the upload token", uploads)
	}
	assertSSECWireHeaders(t, uploads[0].headers, wire.keyValue, wire.keyMD5)

	plainWriter := bucket.Object("plain").NewWriter(ctx)
	if _, err := plainWriter.Write([]byte("plain")); err != nil {
		t.Fatal(err)
	}
	if err := plainWriter.Close(); err != nil {
		t.Fatal(err)
	}
	uploads = wire.requestsTo("/upload")
	if len(uploads) != 2 {
		t.Fatalf("upload requests = %d, want 2", len(uploads))
	}
	assertNoSSECWireHeaders(t, uploads[1].headers)

	reader := encrypted.NewReader(ctx, wire.key)
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("download = %q, want %q", got, payload)
	}
	downloads := wire.requestsTo("/file/bucket/encrypted")
	if len(downloads) != 2 || downloads[0].method != http.MethodGet || downloads[1].method != http.MethodGet {
		t.Fatalf("download requests = %#v, want content GET then end-of-file GET", downloads)
	}
	assertSSECWireHeaders(t, downloads[0].headers, wire.keyValue, wire.keyMD5)
	assertSSECWireHeaders(t, downloads[1].headers, wire.keyValue, wire.keyMD5)

	if _, err := encrypted.Attrs(ctx, wire.key); err != nil {
		t.Fatal(err)
	}
	downloads = wire.requestsTo("/file/bucket/encrypted")
	if len(downloads) != 3 || downloads[2].method != http.MethodHead {
		t.Fatalf("download requests = %#v, want content GET, end-of-file GET, then HEAD", downloads)
	}
	assertSSECWireHeaders(t, downloads[2].headers, wire.keyValue, wire.keyMD5)

	if _, err := bucket.Copy(ctx, "source-id", "copy", CopySourceSSECKey(wire.key), CopyDestinationSSECKey(wire.key)); err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Copy(ctx, "source-id", "plain-copy"); err != nil {
		t.Fatal(err)
	}
	copies := wire.requestsTo("/b2api/v4/b2_copy_file")
	if len(copies) != 2 {
		t.Fatalf("copy requests = %d, want 2", len(copies))
	}
	assertNoSSECWireHeaders(t, copies[0].headers)
	for _, field := range []string{"sourceServerSideEncryption", "destinationServerSideEncryption"} {
		got, ok := copies[0].body[field].(map[string]any)
		if !ok || got["mode"] != "SSE-C" || got["algorithm"] != "AES256" || got["customerKey"] != wire.keyValue || got["customerKeyMd5"] != wire.keyMD5 {
			t.Errorf("%s = %#v, want SSE-C key and MD5", field, copies[0].body[field])
		}
		if _, ok := copies[1].body[field]; ok {
			t.Errorf("plain copy unexpectedly sent %s = %#v", field, copies[1].body[field])
		}
	}
}

func TestSSECReadRejectsMissingAndWrongKeys(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	ctx := context.Background()
	wrong, err := NewSSECKey([]byte("abcdef0123456789abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		newReader func() *Reader
		want      string
	}{
		{"missing", func() *Reader { return bucket.Object("encrypted").NewReader(ctx) }, "SSE-C key required"},
		{"wrong", func() *Reader { return bucket.Object("encrypted").NewReader(ctx, wrong) }, "SSE-C key required"},
		{"nil", func() *Reader { return bucket.Object("encrypted").NewReader(ctx, nil) }, "SSE-C key is nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := tc.newReader()
			_, err := io.ReadAll(reader)
			if closeErr := reader.Close(); err == nil {
				err = closeErr
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("read error = %v, want %q", err, tc.want)
			}
		})
	}
}
