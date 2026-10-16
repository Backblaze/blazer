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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
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

	failCopyPart int
	calls        []string
	copies       []map[string]any
	uploads      []composeUpload
	finishes     []map[string]any
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
		w.addCall("start")
		_, _ = fmt.Fprint(rw, `{"fileId":"large"}`)
	case "/b2api/v4/b2_get_upload_part_url":
		_, _ = fmt.Fprintf(rw, `{"uploadUrl":%q,"authorizationToken":"part-token"}`, w.srv.URL+"/part")
	case "/part":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.t.Error(err)
		}
		number, err := strconv.Atoi(r.Header.Get("X-Bz-Part-Number"))
		if err != nil {
			w.t.Errorf("part number: %v", err)
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "part-token" {
			w.t.Errorf("upload part method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		w.mu.Lock()
		w.uploads = append(w.uploads, composeUpload{number: number, sha1: r.Header.Get("X-Bz-Content-Sha1"), size: len(body)})
		w.calls = append(w.calls, fmt.Sprintf("upload:%d", number))
		w.mu.Unlock()
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
		_, _ = fmt.Fprintf(rw, `{"fileId":"large","partNumber":%d,"contentLength":3,"contentSha1":%q}`, partNumber, fmt.Sprintf("copy-sha-%d", partNumber))
	case "/b2api/v4/b2_finish_large_file":
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.t.Errorf("decode finish: %v", err)
		}
		w.mu.Lock()
		w.finishes = append(w.finishes, body)
		w.calls = append(w.calls, "finish")
		w.mu.Unlock()
		_, _ = fmt.Fprint(rw, `{"fileId":"large","fileName":"composed","action":"upload"}`)
	case "/b2api/v4/b2_cancel_large_file":
		w.addCall("cancel")
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
