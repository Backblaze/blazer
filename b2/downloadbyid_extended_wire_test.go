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
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/base"
)

type downloadByIDRequest struct {
	method  string
	query   string
	headers http.Header
}

type downloadByIDWire struct {
	t *testing.T

	srv *httptest.Server
	mu  sync.Mutex

	status   int
	requests []downloadByIDRequest
}

func newDownloadByIDWire(t *testing.T) *downloadByIDWire {
	t.Helper()
	w := &downloadByIDWire{t: t, status: http.StatusOK}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serveHTTP))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *downloadByIDWire) serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/b2api/v4/b2_authorize_account":
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
		return
	case "/b2api/v4/b2_list_buckets":
		_, _ = fmt.Fprint(rw, `{"buckets":[]}`)
		return
	case "/b2api/v4/b2_create_bucket":
		_, _ = fmt.Fprint(rw, noReplicationBucketJSON)
		return
	case "/b2api/v4/b2_download_file_by_id":
		w.mu.Lock()
		status := w.status
		w.requests = append(w.requests, downloadByIDRequest{method: r.Method, query: r.URL.RawQuery, headers: r.Header.Clone()})
		w.mu.Unlock()
		if status != http.StatusOK && status != http.StatusPartialContent {
			rw.WriteHeader(status)
			_, _ = fmt.Fprintf(rw, `{"status":%d,"code":"download_error","message":"download failed"}`, status)
			return
		}
		rw.Header().Set("Content-Length", "2")
		rw.Header().Set("Content-Type", "application/octet-stream")
		rw.Header().Set("X-Bz-File-Id", "file-id")
		if status == http.StatusPartialContent {
			rw.WriteHeader(status)
		}
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprint(rw, "ok")
		}
		return
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		http.NotFound(rw, r)
	}
}

func (w *downloadByIDWire) setStatus(status int) {
	w.mu.Lock()
	w.status = status
	w.mu.Unlock()
}

func (w *downloadByIDWire) requestsSince(index int) []downloadByIDRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]downloadByIDRequest(nil), w.requests[index:]...)
}

func (w *downloadByIDWire) requestCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.requests)
}

func (w *downloadByIDWire) publicBucket(t *testing.T) *Bucket {
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

func (w *downloadByIDWire) baselineBucket(t *testing.T) *b2Bucket {
	t.Helper()
	account, err := base.AuthorizeAccount(context.Background(), "account", "key", base.SetAPIBase(w.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := account.CreateBucket(context.Background(), "bucket", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return &b2Bucket{b: bucket}
}

func TestDownloadFileByIDPublicRoundTripWire(t *testing.T) {
	wire := newDownloadByIDWire(t)
	reader, err := wire.publicBucket(t).DownloadFileByID(context.Background(), "file-id")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(got) != "ok" {
		t.Errorf("body = %q, want ok", got)
	}
	requests := wire.requestsSince(0)
	if len(requests) != 1 {
		t.Fatalf("download requests = %#v, want one", requests)
	}
	request := requests[0]
	if request.method != http.MethodGet || request.query != "fileId=file-id" || request.headers.Get("Authorization") != "t" {
		t.Errorf("request = %#v, want GET ?fileId=file-id with the account token", request)
	}
}

func TestDownloadFileByIDHeadAndRangeWire(t *testing.T) {
	wire := newDownloadByIDWire(t)
	bucket := wire.baselineBucket(t)
	ctx := context.Background()

	reader, err := bucket.downloadFileByID(ctx, "file-id", 2, 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err = bucket.downloadFileByID(ctx, "file-id", 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	requests := wire.requestsSince(0)
	if len(requests) != 2 {
		t.Fatalf("download requests = %#v, want range GET then HEAD", requests)
	}
	if requests[0].method != http.MethodGet || requests[0].headers.Get("Range") != "bytes=2-4" {
		t.Errorf("range request = %#v, want GET with Range bytes=2-4", requests[0])
	}
	if requests[0].headers.Get("Authorization") != "t" {
		t.Errorf("range request Authorization = %q, want account token", requests[0].headers.Get("Authorization"))
	}
	if requests[1].method != http.MethodHead || requests[1].headers.Get("Range") != "" {
		t.Errorf("HEAD request = %#v, want HEAD without Range", requests[1])
	}
	if requests[1].headers.Get("Authorization") != "t" {
		t.Errorf("HEAD request Authorization = %q, want account token", requests[1].headers.Get("Authorization"))
	}
}

func TestDownloadFileByIDMapsStatusAndInvalidIDErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		id         string
		status     int
		wantAbsent bool
		wantNoMore bool
	}{
		{"not found", "missing", http.StatusNotFound, true, false},
		{"range exhausted", "file-id", http.StatusRequestedRangeNotSatisfiable, false, true},
		{"empty id", "", http.StatusBadRequest, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newDownloadByIDWire(t)
			wire.setStatus(tc.status)
			before := wire.requestCount()
			_, err := wire.baselineBucket(t).downloadFileByID(context.Background(), tc.id, 0, 0, false)
			if err == nil {
				t.Fatal("DownloadFileByID succeeded, want an error")
			}
			if IsNotExist(err) != tc.wantAbsent {
				t.Errorf("IsNotExist(%v) = %v, want %v", err, IsNotExist(err), tc.wantAbsent)
			}
			if (err == errNoMoreContent) != tc.wantNoMore {
				t.Errorf("error = %v, want errNoMoreContent=%v", err, tc.wantNoMore)
			}
			requests := wire.requestsSince(before)
			if len(requests) != 1 {
				t.Fatalf("download requests = %#v, want one", requests)
			}
			if tc.id == "" && !strings.HasPrefix(requests[0].query, "fileId=") {
				t.Errorf("empty-id query = %q, want fileId=", requests[0].query)
			}
		})
	}
}
