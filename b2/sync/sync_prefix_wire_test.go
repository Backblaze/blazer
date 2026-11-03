package sync

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestSyncListsOnlyTheDestinationPrefix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK,
		`{"buckets":[{"bucketId":"bid","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[]}]}`)
	// https://www.backblaze.com/apidocs/b2-list-file-names
	// The root file matches the local size and mtime, so an unscoped listing
	// would incorrectly skip uploading the missing p/a.txt.
	srv.Handle("b2_list_file_names", func(r b2wiretest.Request) b2wiretest.Response {
		if prefix, _ := r.JSON["prefix"].(string); prefix == "" {
			return b2wiretest.Response{Status: http.StatusOK,
				Body: `{"files":[{"fileId":"root-id","fileName":"a.txt","action":"upload","contentLength":5,"contentSha1":"aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d","contentType":"text/plain","fileInfo":{"src_last_modified_millis":"1700000000000"},"uploadTimestamp":1700000000000}],"nextFileName":null}`}
		}
		return b2wiretest.Response{Status: http.StatusOK, Body: `{"files":[],"nextFileName":null}`}
	})
	srv.Reply("upload_file", http.StatusOK,
		`{"fileId":"uploaded-id","fileName":"p/a.txt","action":"upload","contentLength":5,"contentSha1":"aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d","contentType":"text/plain","fileInfo":{"src_last_modified_millis":"1700000000000"},"uploadTimestamp":1700000000000}`)
	client, err := b2.NewClient(ctx, "a", "k", b2.APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.UnixMilli(1700000000000)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(ctx, bucket, "p/", dir); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("b2_list_file_names")
	if len(calls) != 1 {
		t.Fatalf("listing requests = %d, want 1", len(calls))
	}
	b2wiretest.AssertJSON(t, calls[0], "prefix", "p/")
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1 for p/a.txt", len(uploads))
	}
	// https://www.backblaze.com/apidocs/b2-upload-file
	b2wiretest.AssertHeader(t, uploads[0], "X-Bz-File-Name", "p/a.txt")
}
