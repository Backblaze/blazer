package b2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// After a connection fault on an upload, B2's Integration Checklist asks the
// client to fetch a new upload URL rather than retry the broken one. The first
// upload connection is dropped; the retry must go to a fresh URL.
func TestSmallUploadFetchesNewURLAfterConnectionFault(t *testing.T) {
	var (
		mu         sync.Mutex
		urlsIssued int
		uploadPath []string
	)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprintf(w, `{"buckets":[%s]}`, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_get_upload_url"):
			mu.Lock()
			urlsIssued++
			n := urlsIssued
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"bucketId":"bid","uploadUrl":"%s/upload/%d","authorizationToken":"token-%d"}`, srv.URL, n, n)
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			mu.Lock()
			uploadPath = append(uploadPath, r.URL.Path)
			attempt := len(uploadPath)
			mu.Unlock()
			if attempt == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack: %v", err)
					return
				}
				_ = conn.Close() // the client sees a broken connection, not an HTTP response
				return
			}
			_, _ = fmt.Fprint(w, `{"fileId":"fid","fileName":"name","accountId":"a","bucketId":"bid","contentLength":5,"contentSha1":"x","contentType":"application/octet-stream","fileInfo":{},"action":"upload","uploadTimestamp":1}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("name").NewWriter(ctx)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("upload after a connection fault failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if urlsIssued < 2 {
		t.Errorf("b2_get_upload_url called %d time(s), want a fresh upload URL after the fault", urlsIssued)
	}
	if len(uploadPath) != 2 || uploadPath[0] == uploadPath[1] {
		t.Errorf("uploads went to %v, want two different upload URLs", uploadPath)
	}
}

// An HTTP response with Retry-After is not a broken connection: the retry waits
// as told and goes to the same upload URL.
func TestSmallUploadKeepsURLOnRetryAfter(t *testing.T) {
	var (
		mu         sync.Mutex
		urlsIssued int
		uploadPath []string
		uploadTime []time.Time
	)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprintf(w, `{"buckets":[%s]}`, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_get_upload_url"):
			mu.Lock()
			urlsIssued++
			n := urlsIssued
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"bucketId":"bid","uploadUrl":"%s/upload/%d","authorizationToken":"token-%d"}`, srv.URL, n, n)
		case strings.HasPrefix(r.URL.Path, "/upload/"):
			mu.Lock()
			uploadPath = append(uploadPath, r.URL.Path)
			uploadTime = append(uploadTime, time.Now())
			attempt := len(uploadPath)
			mu.Unlock()
			if attempt == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, `{"status":503,"code":"service_unavailable","message":"try later"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"fileId":"fid","fileName":"name","accountId":"a","bucketId":"bid","contentLength":5,"contentSha1":"x","contentType":"application/octet-stream","fileInfo":{},"action":"upload","uploadTimestamp":1}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("name").NewWriter(ctx)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("upload after a 503 with Retry-After failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if urlsIssued != 1 {
		t.Errorf("b2_get_upload_url called %d times, want 1: a 503 with Retry-After must keep the same URL", urlsIssued)
	}
	if len(uploadPath) != 2 || uploadPath[0] != uploadPath[1] {
		t.Errorf("uploads went to %v, want the same URL twice", uploadPath)
	}
	if len(uploadTime) == 2 {
		if waited := uploadTime[1].Sub(uploadTime[0]); waited < 900*time.Millisecond {
			t.Errorf("retried after %v, want at least the 1s Retry-After", waited)
		}
	}
}
