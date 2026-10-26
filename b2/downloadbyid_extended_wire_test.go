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
	"errors"
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
	path    string
	query   string
	headers http.Header
}

type downloadByIDWire struct {
	t *testing.T

	srv *httptest.Server
	mu  sync.Mutex

	status           int
	requests         []downloadByIDRequest
	authorizations   int
	unauthorizedOnce bool
	partialBody      bool
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
		w.mu.Lock()
		w.authorizations++
		w.mu.Unlock()
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
		if w.unauthorizedOnce {
			status = http.StatusUnauthorized
			w.unauthorizedOnce = false
		}
		partialBody := w.partialBody
		w.requests = append(w.requests, downloadByIDRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, headers: r.Header.Clone()})
		w.mu.Unlock()
		if status == http.StatusUnauthorized {
			rw.WriteHeader(status)
			_, _ = fmt.Fprint(rw, `{"status":401,"code":"expired_auth_token","message":"expired"}`)
			return
		}
		if status != http.StatusOK && status != http.StatusPartialContent {
			rw.WriteHeader(status)
			_, _ = fmt.Fprintf(rw, `{"status":%d,"code":"download_error","message":"download failed"}`, status)
			return
		}
		body, ok := map[string]string{"file-id": "ok", "historical-id": "old", "current-id": "new"}[r.URL.Query().Get("fileId")]
		if !ok {
			rw.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(rw, `{"status":404,"code":"not_found","message":"file ID not found"}`)
			return
		}
		if partialBody {
			rw.Header().Set("Content-Length", "10")
		} else {
			rw.Header().Set("Content-Length", fmt.Sprint(len(body)))
		}
		rw.Header().Set("Content-Type", "application/octet-stream")
		rw.Header().Set("X-Bz-File-Id", r.URL.Query().Get("fileId"))
		if status == http.StatusPartialContent {
			rw.WriteHeader(status)
		}
		if r.Method != http.MethodHead {
			_, _ = fmt.Fprint(rw, body)
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
	bucket, err := account.CreateBucket(context.Background(), "bucket", "allPrivate", nil, nil, nil, nil, false, nil)
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
	if request.method != http.MethodGet || request.path != "/b2api/v4/b2_download_file_by_id" || request.query != "fileId=file-id" || request.headers.Get("Authorization") != "t" {
		t.Errorf("request = %#v, want GET ?fileId=file-id with the account token", request)
	}
}

func TestDownloadFileByIDHeadAndRangeWire(t *testing.T) {
	wire := newDownloadByIDWire(t)
	bucket := wire.baselineBucket(t)
	ctx := context.Background()

	reader, err := bucket.downloadFileByID(ctx, "file-id", 2, 3, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err = bucket.downloadFileByID(ctx, "file-id", 0, 0, true, nil)
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

func TestDownloadFileByIDNewClientRangeWire(t *testing.T) {
	wire := newDownloadByIDWire(t)
	bucket := wire.publicBucket(t)
	reader, err := bucket.b.downloadFileByID(context.Background(), "historical-id", 1, 2, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	requests := wire.requestsSince(0)
	if len(requests) != 1 || requests[0].method != http.MethodGet || requests[0].path != "/b2api/v4/b2_download_file_by_id" || requests[0].query != "fileId=historical-id" || requests[0].headers.Get("Range") != "bytes=1-2" || requests[0].headers.Get("Authorization") != "t" {
		t.Fatalf("NewClient range request = %#v, want GET by historical ID with Range and auth", requests)
	}
}

func TestDownloadFileByIDMapsStatusAndInvalidIDErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		id         string
		status     int
		offset     int64
		size       int64
		wantAbsent bool
		wantNoMore bool
	}{
		{"not found", "missing", http.StatusNotFound, 0, 0, true, false},
		{"range exhausted", "file-id", http.StatusRequestedRangeNotSatisfiable, 50, 5, false, true},
		{"empty id", "", http.StatusBadRequest, 0, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newDownloadByIDWire(t)
			wire.setStatus(tc.status)
			before := wire.requestCount()
			_, err := wire.baselineBucket(t).downloadFileByID(context.Background(), tc.id, tc.offset, tc.size, false, nil)
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
			if tc.wantNoMore && requests[0].headers.Get("Range") != "bytes=50-54" {
				t.Errorf("past-EOF request Range = %q, want bytes=50-54", requests[0].headers.Get("Range"))
			}
		})
	}
}

func TestDownloadFileByIDHistoricalVersionDoesNotUseName(t *testing.T) {
	wire := newDownloadByIDWire(t)
	bucket := wire.publicBucket(t)
	for _, tc := range []struct{ id, want string }{{"current-id", "new"}, {"historical-id", "old"}} {
		reader, err := bucket.DownloadFileByID(context.Background(), tc.id)
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: read=%v close=%v", tc.id, readErr, closeErr)
		}
		if string(got) != tc.want {
			t.Fatalf("id %s returned %q, want %q", tc.id, got, tc.want)
		}
	}
	requests := wire.requestsSince(0)
	if len(requests) != 2 || requests[0].query != "fileId=current-id" || requests[1].query != "fileId=historical-id" {
		t.Fatalf("version requests = %#v, want current and historical IDs", requests)
	}
}

func TestDownloadFileByIDReauthorizesOn401(t *testing.T) {
	wire := newDownloadByIDWire(t)
	bucket := wire.publicBucket(t)
	wire.mu.Lock()
	wire.unauthorizedOnce = true
	wire.mu.Unlock()
	reader, err := bucket.DownloadFileByID(context.Background(), "file-id")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(got) != "ok" {
		t.Fatalf("recovered download = %q, read=%v close=%v", got, readErr, closeErr)
	}
	wire.mu.Lock()
	authorizations := wire.authorizations
	wire.mu.Unlock()
	if authorizations != 2 || wire.requestCount() != 2 {
		t.Fatalf("authorizations=%d downloads=%d, want initial+refresh and failed+retried", authorizations, wire.requestCount())
	}
}

func TestDownloadFileByIDPartialBodyReportsError(t *testing.T) {
	wire := newDownloadByIDWire(t)
	wire.partialBody = true
	bucket := wire.publicBucket(t)
	reader, err := bucket.DownloadFileByID(context.Background(), "file-id")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if !errors.Is(readErr, io.ErrUnexpectedEOF) || string(got) != "ok" || closeErr != nil {
		t.Fatalf("partial download = %q, read=%v close=%v; want unexpected EOF", got, readErr, closeErr)
	}
	if wire.requestCount() != 1 {
		t.Fatalf("partial download made %d requests, want one single-request read", wire.requestCount())
	}
	wire.mu.Lock()
	wire.partialBody = false
	wire.mu.Unlock()
	retry, err := bucket.DownloadFileByID(context.Background(), "file-id")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr = io.ReadAll(retry)
	closeErr = retry.Close()
	if readErr != nil || closeErr != nil || string(got) != "ok" || wire.requestCount() != 2 {
		t.Fatalf("caller retry = %q, read=%v close=%v requests=%d; want successful second request", got, readErr, closeErr, wire.requestCount())
	}
}
