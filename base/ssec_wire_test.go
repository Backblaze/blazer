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

package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

type ssecWireRequest struct {
	path    string
	headers http.Header
	body    map[string]any
}

func newSSECWireFixture(t *testing.T) (*B2, *Bucket, *httptest.Server, *[]ssecWireRequest) {
	t.Helper()
	requests := []ssecWireRequest{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b2api/v4/b2_authorize_account" {
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
			return
		}
		recorded := ssecWireRequest{path: r.URL.Path, headers: r.Header.Clone()}
		if strings.HasPrefix(r.URL.Path, "/b2api/") {
			recorded.body = map[string]any{}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &recorded.body); err != nil {
				t.Errorf("decode %s body: %v", r.URL.Path, err)
			}
		}
		requests = append(requests, recorded)
		switch {
		case r.URL.Path == "/upload":
			_, _ = fmt.Fprint(w, `{"fileId":"small","fileName":"n","action":"upload"}`)
		case r.URL.Path == "/part":
			_, _ = fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/b2_start_large_file"):
			_, _ = fmt.Fprint(w, `{"fileId":"large"}`)
		case strings.HasSuffix(r.URL.Path, "/b2_copy_file"):
			_, _ = fmt.Fprint(w, `{"fileId":"copy","fileName":"dst","action":"copy","contentLength":1,"uploadTimestamp":1}`)
		case strings.HasPrefix(r.URL.Path, "/file/bucket/"):
			w.Header().Set("Content-Length", "1")
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("X-Bz-File-Id", "small")
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte("x"))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return b, &Bucket{ID: "bucket-id", Name: "bucket", b2: b}, srv, &requests
}

func testSSEC(key, digest string) *b2types.ServerSideEncryption {
	return &b2types.ServerSideEncryption{
		Mode:           "SSE-C",
		Algorithm:      "AES256",
		CustomerKey:    key,
		CustomerKeyMD5: digest,
	}
}

func assertSSECHeaders(t *testing.T, headers http.Header, key, digest string) {
	t.Helper()
	want := map[string]string{
		"X-Bz-Server-Side-Encryption-Customer-Algorithm": "AES256",
		"X-Bz-Server-Side-Encryption-Customer-Key":       key,
		"X-Bz-Server-Side-Encryption-Customer-Key-Md5":   digest,
	}
	for name, value := range want {
		if got := headers.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func assertSSECBody(t *testing.T, got any, key, digest string) {
	t.Helper()
	object, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("encryption body = %#v, want an object", got)
	}
	want := map[string]string{"mode": "SSE-C", "algorithm": "AES256", "customerKey": key, "customerKeyMd5": digest}
	for name, value := range want {
		if got := object[name]; got != value {
			t.Errorf("encryption.%s = %v, want %q", name, got, value)
		}
	}
}

func TestSSECWireShapes(t *testing.T) {
	b, bucket, srv, requests := newSSECWireFixture(t)
	ctx := context.Background()
	enc := testSSEC("key-base64", "md5-base64")

	uploadURL := &URL{uri: srv.URL + "/upload", token: "upload-token", b2: b}
	if _, err := uploadURL.UploadFileWithSSEC(ctx, strings.NewReader("x"), 1, "n", "text/plain", "sha1", nil, enc); err != nil {
		t.Fatal(err)
	}
	assertSSECHeaders(t, (*requests)[len(*requests)-1].headers, "key-base64", "md5-base64")

	if _, err := bucket.StartLargeFileWithSSEC(ctx, "n", "text/plain", nil, enc); err != nil {
		t.Fatal(err)
	}
	assertSSECBody(t, (*requests)[len(*requests)-1].body["serverSideEncryption"], "key-base64", "md5-base64")

	chunk := &FileChunk{url: srv.URL + "/part", token: "part-token", file: &LargeFile{b2: b, hashes: map[int]string{}}}
	if _, err := chunk.UploadPartWithSSEC(ctx, strings.NewReader("x"), "sha1", 1, 1, enc); err != nil {
		t.Fatal(err)
	}
	assertSSECHeaders(t, (*requests)[len(*requests)-1].headers, "key-base64", "md5-base64")

	reader, err := bucket.DownloadFileByNameWithSSEC(ctx, "n", 0, 0, true, enc)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	assertSSECHeaders(t, (*requests)[len(*requests)-1].headers, "key-base64", "md5-base64")
	if got := (*requests)[len(*requests)-1].headers.Get("Range"); got != "" {
		t.Errorf("HEAD Range = %q, want empty", got)
	}

	reader, err = bucket.DownloadFileByNameWithSSEC(ctx, "n", 2, 3, false, enc)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	assertSSECHeaders(t, (*requests)[len(*requests)-1].headers, "key-base64", "md5-base64")
	if got := (*requests)[len(*requests)-1].headers.Get("Range"); got != "bytes=2-4" {
		t.Errorf("GET Range = %q, want bytes=2-4", got)
	}

	source := testSSEC("source-key", "source-md5")
	destination := testSSEC("destination-key", "destination-md5")
	if _, err := bucket.CopyFileWithSSEC(ctx, "source-id", "dst", "", "", "COPY", "", nil, source, destination); err != nil {
		t.Fatal(err)
	}
	copyBody := (*requests)[len(*requests)-1].body
	assertSSECBody(t, copyBody["sourceServerSideEncryption"], "source-key", "source-md5")
	assertSSECBody(t, copyBody["destinationServerSideEncryption"], "destination-key", "destination-md5")
}

func TestSSEB2AndSSECConflictIsRejected(t *testing.T) {
	conflict := &b2types.ServerSideEncryption{
		Mode:           "SSE-B2",
		Algorithm:      "AES256",
		CustomerKey:    "secret",
		CustomerKeyMD5: "digest",
	}
	err := addServerSideEncryptionHeaders(map[string]string{}, conflict)
	if err == nil || !strings.Contains(err.Error(), "SSE-B2 and SSE-C") {
		t.Fatalf("conflict error = %v, want a clear SSE-B2/SSE-C conflict", err)
	}
}

func TestSSEB2JSONIsUnchanged(t *testing.T) {
	got, err := json.Marshal(&b2types.ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"mode":"SSE-B2","algorithm":"AES256"}`; string(got) != want {
		t.Errorf("SSE-B2 JSON = %s, want %s", got, want)
	}
}

func TestSSECLoggingRedactsKeyMaterial(t *testing.T) {
	if got := redactSSECHeader("X-Bz-Server-Side-Encryption-Customer-Key", "header-secret"); got != "[REDACTED]" {
		t.Errorf("key header redaction = %q", got)
	}
	if got := redactSSECHeader("X-Bz-Server-Side-Encryption-Customer-Key-Md5", "digest-secret"); got != "[REDACTED]" {
		t.Errorf("digest header redaction = %q", got)
	}
	if got := redactSSECHeader("X-Bz-Server-Side-Encryption-Customer-Algorithm", "AES256"); got != "AES256" {
		t.Errorf("algorithm header redaction = %q, want AES256", got)
	}

	raw := []byte(`{"sourceServerSideEncryption":{"mode":"SSE-C","customerKey":"body-secret","customerKeyMd5":"body-digest"},"destinationServerSideEncryption":{"customerKey":"destination-secret"}}`)
	redacted := string(redactSSECJSON(raw))
	for _, secret := range []string{"body-secret", "body-digest", "destination-secret"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("redacted JSON disclosed %q", secret)
		}
	}
	if count := strings.Count(redacted, "[REDACTED]"); count != 3 {
		t.Errorf("redacted JSON = %s, want three redactions", redacted)
	}
}
