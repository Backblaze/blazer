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
	"crypto/sha1"
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

	mu                   sync.Mutex
	requests             []ssecPublicRequest
	objects              map[string][]byte
	unfinishedEncryption string
	partsResponse        string
	authorizeCalls       int
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
		w.mu.Lock()
		w.authorizeCalls++
		w.mu.Unlock()
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
		return
	}
	recorded := ssecPublicRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone()}
	if strings.HasPrefix(r.URL.Path, "/b2api/") && r.Method != http.MethodGet {
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
	case strings.HasSuffix(r.URL.Path, "/b2_start_large_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"large-id"}`)
	case strings.HasSuffix(r.URL.Path, "/b2_get_upload_part_url"):
		_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":"part-token"}`, w.srv.URL+"/upload-part")
	case r.URL.Path == "/upload-part":
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.t.Error(err)
		}
		_, _ = fmt.Fprint(rw, `{}`)
	case strings.HasSuffix(r.URL.Path, "/b2_finish_large_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"large-id","fileName":"multipart","action":"upload","uploadTimestamp":1}`)
	case strings.HasSuffix(r.URL.Path, "/b2_download_file_by_id"):
		if r.URL.Query().Get("fileId") != "id-encrypted" {
			w.t.Errorf("by-ID download requested %q, want id-encrypted", r.URL.Query().Get("fileId"))
		}
		w.serveDownload(rw, r, "encrypted")
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
		w.serveDownload(rw, r, strings.TrimPrefix(r.URL.Path, "/file/bucket/"))
	case strings.HasSuffix(r.URL.Path, "/b2_get_file_info"):
		if recorded.body["fileId"] == "historical-id" {
			_, _ = fmt.Fprint(rw, `{"fileId":"historical-id","fileName":"encrypted","action":"upload","contentLength":13,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{"version":"historical"},"uploadTimestamp":1}`)
		} else {
			_, _ = fmt.Fprint(rw, `{"fileId":"id-encrypted","fileName":"encrypted","action":"upload","contentLength":7,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`)
		}
	case strings.HasSuffix(r.URL.Path, "/b2_list_unfinished_large_files"):
		_, _ = fmt.Fprintf(rw, `{"files":[{"fileId":"unfinished-id","fileName":"encrypted","action":"start","contentType":"application/octet-stream","fileInfo":{},"serverSideEncryption":%s}],"nextFileId":null}`, w.unfinishedEncryption)
	case strings.HasSuffix(r.URL.Path, "/b2_list_parts"):
		if w.partsResponse != "" {
			_, _ = fmt.Fprint(rw, w.partsResponse)
		} else {
			_, _ = fmt.Fprint(rw, `{"parts":[],"nextPartNumber":null}`)
		}
	case strings.HasSuffix(r.URL.Path, "/b2_copy_file"):
		_, _ = fmt.Fprint(rw, `{"fileId":"copy-id","fileName":"copy","action":"copy","contentLength":7,"contentSha1":"0000000000000000000000000000000000000000","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`)
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(rw, r)
	}
}

func (w *ssecPublicWire) serveDownload(rw http.ResponseWriter, r *http.Request, name string) {
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
			t.Errorf("%s does not match the requested SSE-C value", name)
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
			t.Errorf("unexpected %s header is present", name)
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
		t.Fatalf("upload request count/auth mismatch: got %d requests", len(uploads))
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
		t.Fatalf("download request count/method mismatch: got %d requests", len(downloads))
	}
	assertSSECWireHeaders(t, downloads[0].headers, wire.keyValue, wire.keyMD5)
	assertSSECWireHeaders(t, downloads[1].headers, wire.keyValue, wire.keyMD5)

	if _, err := bucket.Object("encrypted").Attrs(ctx, wire.key); err != nil {
		t.Fatal(err)
	}
	downloads = wire.requestsTo("/file/bucket/encrypted")
	if len(downloads) != 3 || downloads[2].method != http.MethodHead {
		t.Fatalf("download request count/method mismatch after HEAD: got %d requests", len(downloads))
	}
	assertSSECWireHeaders(t, downloads[2].headers, wire.keyValue, wire.keyMD5)
	if _, err := encrypted.Attrs(ctx, wire.key); err != nil {
		t.Fatal(err)
	}
	if got := len(wire.requestsTo("/file/bucket/encrypted")); got != 3 {
		t.Fatalf("resolved version caused %d name downloads, want 3", got)
	}
	infos := wire.requestsTo("/b2api/v4/b2_get_file_info")
	if len(infos) != 2 || infos[1].body["fileId"] != "id-encrypted" {
		t.Fatalf("resolved version file-info requests = %#v, want by-ID lookup", infos)
	}
	historical := &Object{name: "encrypted", b: bucket, f: bucket.b.file("historical-id", "encrypted")}
	historicalAttrs, err := historical.Attrs(ctx, wire.key)
	if err != nil {
		t.Fatal(err)
	}
	if historicalAttrs.Size != 13 || historicalAttrs.Info["version"] != "historical" {
		t.Fatalf("historical attrs = %#v, want historical version metadata", historicalAttrs)
	}
	infos = wire.requestsTo("/b2api/v4/b2_get_file_info")
	if len(infos) != 3 || infos[2].body["fileId"] != "historical-id" {
		t.Fatalf("historical version file-info requests = %#v, want historical ID", infos)
	}

	destinationKey, err := NewSSECKey([]byte("abcdef0123456789abcdef0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	destinationEncoded, destinationMD5, err := destinationKey.encoded()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Copy(ctx, "source-id", "copy", CopySourceSSECKey(wire.key), CopyDestinationSSECKey(destinationKey)); err != nil {
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
	for field, key := range map[string][2]string{"sourceServerSideEncryption": {wire.keyValue, wire.keyMD5}, "destinationServerSideEncryption": {destinationEncoded, destinationMD5}} {
		got, ok := copies[0].body[field].(map[string]any)
		if !ok || got["mode"] != "SSE-C" || got["algorithm"] != "AES256" || got["customerKey"] != key[0] || got["customerKeyMd5"] != key[1] {
			t.Errorf("%s does not match its requested SSE-C key and digest", field)
		}
		if _, ok := copies[1].body[field]; ok {
			t.Errorf("plain copy unexpectedly sent %s", field)
		}
	}
}

func TestDownloadFileByIDWithSSECWire(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	ctx := context.Background()
	payload := []byte("encrypted by-id payload")
	object := bucket.Object("encrypted")
	writer := object.NewWriter(ctx, WithSSECKey(wire.key))
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := bucket.DownloadFileByID(ctx, object.ID(), wire.key)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if readErr != nil || !bytes.Equal(got, payload) {
		t.Fatalf("by-ID download = %q, error %v; want %q", got, readErr, payload)
	}
	requests := wire.requestsTo("/b2api/v4/b2_download_file_by_id")
	if len(requests) != 1 || requests[0].method != http.MethodGet {
		t.Fatalf("by-ID requests = %#v, want one GET", requests)
	}
	assertSSECWireHeaders(t, requests[0].headers, wire.keyValue, wire.keyMD5)
	if _, err := bucket.DownloadFileByID(ctx, object.ID()); err == nil {
		t.Fatal("by-ID download without SSE-C key succeeded")
	} else if strings.Contains(err.Error(), wire.keyValue) || strings.Contains(err.Error(), wire.keyMD5) {
		t.Fatalf("by-ID error disclosed key material: %v", err)
	}
	if _, err := bucket.DownloadFileByID(ctx, object.ID(), nil); err == nil {
		t.Fatal("explicit nil SSE-C key was accepted")
	}
	requests = wire.requestsTo("/b2api/v4/b2_download_file_by_id")
	if len(requests) != 2 {
		t.Fatalf("by-ID requests = %d, want keyed and missing-key requests only", len(requests))
	}
	assertNoSSECWireHeaders(t, requests[1].headers)
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

func TestSSECKeyCannotBeClientWriterDefault(t *testing.T) {
	wire := newSSECPublicWire(t)
	_, err := NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL), DefaultWriterOptions(WithSSECKey(wire.key)))
	if err == nil || !strings.Contains(err.Error(), "SSE-C") {
		t.Fatalf("NewClient with SSE-C writer default error = %v, want rejection", err)
	}
	_, err = NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL), DefaultWriterOptions(WithSSECKey(nil)))
	if err == nil || !strings.Contains(err.Error(), "SSE-C") {
		t.Fatalf("NewClient with nil SSE-C writer default error = %v, want rejection", err)
	}
	wire.mu.Lock()
	calls := wire.authorizeCalls
	wire.mu.Unlock()
	if calls != 0 {
		t.Fatalf("invalid SSE-C default caused %d authorization calls, want zero", calls)
	}
}

func TestNilSSECKeyCannotSilentlyUpload(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	w := bucket.Object("nil-key").NewWriter(context.Background(), WithSSECKey(nil))
	_, writeErr := w.Write([]byte("payload"))
	closeErr := w.Close()
	if writeErr == nil && closeErr == nil {
		t.Fatal("WithSSECKey(nil) uploaded without encryption, want rejection")
	}
	if got := len(wire.requestsTo("/upload")); got != 0 {
		t.Fatalf("nil SSE-C key caused %d unencrypted uploads", got)
	}
}

func TestCopyRejectsExplicitNilSSECKeysBeforeRequest(t *testing.T) {
	for _, test := range []struct {
		name   string
		option CopyOption
	}{
		{"source", CopySourceSSECKey(nil)},
		{"destination", CopyDestinationSSECKey(nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := newSSECPublicWire(t)
			bucket := wire.newBucket(t)
			wire.mu.Lock()
			before := len(wire.requests)
			wire.mu.Unlock()
			_, err := bucket.Copy(context.Background(), "source-id", "copy", test.option)
			if err == nil || !strings.Contains(err.Error(), "nil") {
				t.Fatalf("Copy with nil %s key error = %v, want a clear nil-key error", test.name, err)
			}
			wire.mu.Lock()
			after := len(wire.requests)
			wire.mu.Unlock()
			if after != before {
				t.Fatalf("Copy with nil %s key sent %d requests", test.name, after-before)
			}
		})
	}
}

func TestZeroSSECKeyRejectedByPublicOperations(t *testing.T) {
	zero := &SSECKey{}
	tests := []struct {
		name string
		run  func(context.Context, *Bucket) error
	}{
		{"Writer", func(ctx context.Context, bucket *Bucket) error {
			writer := bucket.Object("zero-key").NewWriter(ctx, WithSSECKey(zero))
			_, writeErr := writer.Write([]byte("payload"))
			closeErr := writer.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}},
		{"NewReader", func(ctx context.Context, bucket *Bucket) error {
			reader := bucket.Object("encrypted").NewReader(ctx, zero)
			_, err := reader.Read(make([]byte, 1))
			_ = reader.Close()
			return err
		}},
		{"NewRangeReader", func(ctx context.Context, bucket *Bucket) error {
			reader := bucket.Object("encrypted").NewRangeReader(ctx, 0, 1, zero)
			_, err := reader.Read(make([]byte, 1))
			_ = reader.Close()
			return err
		}},
		{"Attrs", func(ctx context.Context, bucket *Bucket) error {
			_, err := bucket.Object("encrypted").Attrs(ctx, zero)
			return err
		}},
		{"Copy source", func(ctx context.Context, bucket *Bucket) error {
			_, err := bucket.Copy(ctx, "source-id", "copy", CopySourceSSECKey(zero))
			return err
		}},
		{"Copy destination", func(ctx context.Context, bucket *Bucket) error {
			_, err := bucket.Copy(ctx, "source-id", "copy", CopyDestinationSSECKey(zero))
			return err
		}},
		{"DownloadFileByID", func(ctx context.Context, bucket *Bucket) error {
			reader, err := bucket.DownloadFileByID(ctx, "id-encrypted", zero)
			if reader != nil {
				_ = reader.Close()
			}
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := newSSECPublicWire(t)
			bucket := wire.newBucket(t)
			wire.mu.Lock()
			before := len(wire.requests)
			wire.mu.Unlock()
			err := test.run(context.Background(), bucket)
			if err == nil || !strings.Contains(err.Error(), "NewSSECKey") {
				t.Fatalf("%s with zero-value key error = %v, want NewSSECKey guidance", test.name, err)
			}
			wire.mu.Lock()
			after := len(wire.requests)
			wire.mu.Unlock()
			if after != before {
				t.Fatalf("%s with zero-value key sent %d requests", test.name, after-before)
			}
		})
	}
}

func TestOrdinaryWriterOptionRemainsDefaultable(t *testing.T) {
	wire := newSSECPublicWire(t)
	c, err := NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL), DefaultWriterOptions(WithAttrsOption(&Attrs{ContentType: "text/plain"})))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := c.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("plain").NewWriter(context.Background())
	if w.contentType != "text/plain" || w.encryption != nil {
		t.Fatalf("ordinary writer defaults = content type %q, encryption %v", w.contentType, w.encryption)
	}
}

func TestSSECResumeChecksUnfinishedEncryption(t *testing.T) {
	for _, tc := range []struct {
		name string
		sse  string
	}{
		{"unencrypted", `{"mode":null,"algorithm":null}`},
		{"wrong mode", `{"mode":"SSE-B2","algorithm":"AES256"}`},
		{"missing digest", `{"mode":"SSE-C","algorithm":"AES256"}`},
		{"wrong digest", `{"mode":"SSE-C","algorithm":"AES256","customerKeyMd5":"wrong-digest"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newSSECPublicWire(t)
			wire.unfinishedEncryption = tc.sse
			bucket := wire.newBucket(t)
			w := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
			w.Resume = true
			if _, err := w.getLargeFile(); err == nil || !strings.Contains(err.Error(), "SSE-C") {
				t.Fatalf("resuming %s: error = %v, want SSE-C mismatch/unverifiable rejection", tc.name, err)
			}
			if got := len(wire.requestsTo("/b2api/v4/b2_list_parts")); got != 0 {
				t.Fatalf("resuming %s listed %d parts before validating encryption", tc.name, got)
			}
		})
	}
	t.Run("matching digest", func(t *testing.T) {
		wire := newSSECPublicWire(t)
		wire.unfinishedEncryption = fmt.Sprintf(`{"mode":"SSE-C","algorithm":"AES256","customerKeyMd5":%q}`, wire.keyMD5)
		bucket := wire.newBucket(t)
		w := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
		w.Resume = true
		if _, err := w.getLargeFile(); err != nil {
			t.Fatalf("matching SSE-C key digest rejected: %v", err)
		}
		if got := len(wire.requestsTo("/b2api/v4/b2_list_parts")); got != 1 {
			t.Fatalf("matching resume listed %d part pages, want 1", got)
		}
	})
}

func TestSSECResumeWriterFailsClosedBeforePartReuse(t *testing.T) {
	wire := newSSECPublicWire(t)
	wire.unfinishedEncryption = `{"mode":"SSE-C","algorithm":"AES256"}` // B2 did not provide a verifiable key digest.
	first, second := sha1.Sum([]byte("abcde")), sha1.Sum([]byte("fghij"))
	wire.partsResponse = fmt.Sprintf(`{"parts":[{"partNumber":1,"contentSha1":"%x","contentLength":5},{"partNumber":2,"contentSha1":"%x","contentLength":5}],"nextPartNumber":null}`, first, second)
	bucket := wire.newBucket(t)
	w := bucket.Object("encrypted").NewWriter(context.Background(), WithSSECKey(wire.key))
	w.Resume = true
	w.ChunkSize = 5
	_, writeErr := w.Write([]byte("abcdefghij"))
	closeErr := w.Close()
	if writeErr == nil || !strings.Contains(writeErr.Error(), "SSE-C") {
		t.Fatalf("resume Write error = %v, want fail-closed SSE-C rejection", writeErr)
	}
	if closeErr == nil || !strings.Contains(closeErr.Error(), "SSE-C") {
		t.Fatalf("resume Close error = %v, want same rejection", closeErr)
	}
	if got := len(wire.requestsTo("/b2api/v4/b2_finish_large_file")); got != 0 {
		t.Fatalf("resumed unverified file was finished: %d finish calls", got)
	}
	if got := len(wire.requestsTo("/upload-part")); got != 0 {
		t.Fatalf("resumed unverified file uploaded %d parts", got)
	}
}

func TestSSECMultipartSendsKeyOnStartAndEveryPart(t *testing.T) {
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	w := bucket.Object("multipart").NewWriter(context.Background(), WithSSECKey(wire.key))
	w.ChunkSize = 5e6
	w.ConcurrentUploads = 2
	if _, err := w.Write(bytes.Repeat([]byte("x"), 10e6)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	starts := wire.requestsTo("/b2api/v4/b2_start_large_file")
	if len(starts) != 1 {
		t.Fatalf("start calls = %d, want 1", len(starts))
	}
	startSSE, ok := starts[0].body["serverSideEncryption"].(map[string]any)
	if !ok || startSSE["mode"] != "SSE-C" || startSSE["algorithm"] != "AES256" || startSSE["customerKey"] != wire.keyValue || startSSE["customerKeyMd5"] != wire.keyMD5 {
		t.Fatal("start encryption does not match the requested SSE-C key and digest")
	}
	parts := wire.requestsTo("/upload-part")
	if len(parts) != 2 {
		t.Fatalf("part calls = %d, want 2", len(parts))
	}
	seen := map[string]bool{}
	for _, part := range parts {
		if part.headers.Get("Authorization") != "part-token" {
			t.Errorf("part auth = %q, want part-token", part.headers.Get("Authorization"))
		}
		assertSSECWireHeaders(t, part.headers, wire.keyValue, wire.keyMD5)
		seen[part.headers.Get("X-Bz-Part-Number")] = true
	}
	if !seen["1"] || !seen["2"] {
		t.Errorf("part numbers = %#v, want 1 and 2", seen)
	}
}
