package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Exercise both public metadata paths through NewClient and the real HTTP
// stack. The server records requests rather than substituting a fake backend.
func TestAttrsPublicMetadataPathsKeepFileInfoIndependent(t *testing.T) {
	ctx := context.Background()
	type request struct {
		method string
		path   string
		auth   string
		body   map[string]any
	}
	var requests []request
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/b2_authorize_account") {
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
			return
		}
		entry := request{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization")}
		if err := json.NewDecoder(r.Body).Decode(&entry.body); err != nil {
			t.Errorf("decode %s request: %v", r.URL.Path, err)
			return
		}
		requests = append(requests, entry)
		const file = `{"fileId":"old-version","fileName":"file.txt","action":"upload","contentLength":7,"contentSha1":"none","contentType":"text/plain","fileInfo":{"src_last_modified_millis":"1700000000000","large_file_sha1":"whole-file-sha1","custom":"value"},"uploadTimestamp":1700000001000}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprint(w, `{"buckets":[]}`)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			_, _ = fmt.Fprint(w, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_list_file_names"):
			_, _ = fmt.Fprintf(w, `{"files":[%s],"nextFileName":null}`, file)
		case strings.HasSuffix(r.URL.Path, "/b2_list_file_versions"):
			_, _ = fmt.Fprintf(w, `{"files":[%s],"nextFileName":null,"nextFileId":null}`, file)
		case strings.HasSuffix(r.URL.Path, "/b2_get_file_info"):
			_, _ = fmt.Fprint(w, file)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	wantInfo := map[string]string{"large_file_sha1": "whole-file-sha1", "custom": "value"}
	wantTime := time.UnixMilli(1700000000000)
	byName, err := bucket.GetFileInfoByName(ctx, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(byName.Info, wantInfo) || byName.SHA1 != "whole-file-sha1" || !byName.LastModified.Equal(wantTime) {
		t.Fatalf("GetFileInfoByName = %+v, want public metadata with large_file_sha1", byName)
	}

	iter := bucket.List(ctx, ListHidden())
	if !iter.Next() {
		t.Fatalf("ListHidden: %v", iter.Err())
	}
	historical := iter.Object()
	if historical.ID() != "old-version" || iter.Next() || iter.Err() != nil {
		t.Fatalf("historical listing: id=%q err=%v", historical.ID(), iter.Err())
	}
	first, err := historical.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := historical.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Info, wantInfo) || !reflect.DeepEqual(second.Info, wantInfo) || !first.LastModified.Equal(second.LastModified) {
		t.Fatalf("historical Attrs changed: first=%+v second=%+v", first, second)
	}
	first.Info["custom"] = "mutated"
	delete(first.Info, "large_file_sha1")
	third, err := historical.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(third.Info, wantInfo) {
		t.Fatalf("mutating returned Info changed cached historical version: %#v", third.Info)
	}

	// Force an uncached read of the same resolved historical version. It must
	// request b2_get_file_info by ID, never download/HEAD the current name.
	historical.f = bucket.b.file("old-version", "file.txt")
	if _, err := historical.Attrs(ctx); err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"/b2api/v4/b2_list_buckets", "/b2api/v4/b2_create_bucket", "/b2api/v4/b2_list_file_names", "/b2api/v4/b2_list_file_versions", "/b2api/v4/b2_get_file_info"}
	if len(requests) != len(wantPaths) {
		t.Fatalf("requests = %#v, want paths %v", requests, wantPaths)
	}
	for i, req := range requests {
		if req.method != http.MethodPost || req.path != wantPaths[i] || req.auth != "t" {
			t.Errorf("request %d = %#v, want POST %s with account token", i, req, wantPaths[i])
		}
	}
	if got := requests[4].body["fileId"]; got != "old-version" {
		t.Errorf("by-ID lookup body fileId = %v, want old-version", got)
	}
}
