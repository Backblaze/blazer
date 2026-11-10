package sync

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestSyncComparisonModesIgnoreOtherMetadata(t *testing.T) {
	// MODTIME, SIZE and NONE follow the Python comparison policy:
	// https://github.com/Backblaze/b2-sdk-python/blob/master/b2sdk/_internal/sync/policy.py
	for _, test := range []struct {
		name        string
		options     []Option
		remoteSize  int
		remoteMtime int64
	}{
		{"default modtime ignores size", nil, 13, 1700000000000},
		{"explicit modtime ignores size", []Option{WithCompare(CompareModTime)}, 13, 1700000000000},
		{"size ignores mtime", []Option{WithCompare(CompareSize)}, 12, 1700000001000},
		{"sha1 ignores size and mtime", []Option{WithCompare(CompareSHA1)}, 13, 1700000001000},
		{"none ignores size and mtime", []Option{WithCompare(CompareNone)}, 13, 1700000001000},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Reply("b2_list_buckets", http.StatusOK,
				`{"buckets":[{"bucketId":"bid","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[]}]}`)
			// https://www.backblaze.com/apidocs/b2-list-file-names
			srv.Reply("b2_list_file_names", http.StatusOK, fmt.Sprintf(
				`{"files":[{"fileId":"existing-id","fileName":"p/data.txt","action":"upload","contentLength":%d,"contentSha1":"430ce34d020724ed75a196dfc2ad67c77772d169","contentType":"text/plain","fileInfo":{"src_last_modified_millis":"%d"},"uploadTimestamp":1700000000000}],"nextFileName":null}`,
				test.remoteSize, test.remoteMtime))
			// A regression must fail promptly rather than retry an unscripted upload.
			srv.Reply("upload_file", http.StatusBadRequest,
				`{"status":400,"code":"bad_request","message":"unexpected upload of unchanged file"}`)
			client, err := b2.NewClient(ctx, "a", "k", b2.APIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.Bucket(ctx, "bucket")
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path := filepath.Join(root, "data.txt")
			if err := os.WriteFile(path, []byte("hello world!"), 0o600); err != nil {
				t.Fatal(err)
			}
			mtime := time.UnixMilli(1700000000000)
			if err := os.Chtimes(path, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			result, err := Sync(ctx, bucket, "p", root, test.options...)
			if err != nil {
				t.Fatalf("Sync: %v; unchanged file must be skipped", err)
			}
			if result == nil || !slices.Equal(result.Skipped, []string{"data.txt"}) || len(result.Uploaded) != 0 || len(result.Failed) != 0 || result.BytesUploaded != 0 {
				t.Fatalf("Sync = %+v, want only data.txt skipped and zero uploaded bytes", result)
			}
			if calls := srv.Calls("b2_list_file_names"); len(calls) != 1 {
				t.Fatalf("list calls = %d, want 1", len(calls))
			}
			if calls := srv.Calls("b2_get_upload_url"); len(calls) != 0 {
				t.Fatalf("get-upload-url calls = %d, want 0", len(calls))
			}
			if calls := srv.Calls("upload_file"); len(calls) != 0 {
				t.Fatalf("upload calls = %d, want 0", len(calls))
			}
		})
	}
}
