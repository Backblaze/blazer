package b2

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBucketDownloadFileByIDReachesTheWire(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case "/b2api/v4/b2_list_buckets":
			_, _ = fmt.Fprint(w, `{"buckets":[]}`)
		case "/b2api/v4/b2_create_bucket":
			_, _ = fmt.Fprint(w, noReplicationBucketJSON)
		case "/b2api/v4/b2_download_file_by_id":
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			if got := r.URL.Query().Get("fileId"); got != "file-id" {
				t.Errorf("fileId = %q, want file-id", got)
			}
			if got := r.Header.Get("Authorization"); got != "t" {
				t.Errorf("Authorization = %q, want t", got)
			}
			w.Header().Set("Content-Length", "2")
			w.Header().Set("X-Bz-File-Id", "file-id")
			_, _ = fmt.Fprint(w, "ok")
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, "a", "k", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := bucket.DownloadFileByID(ctx, "file-id")
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
}
