package b2

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// cachedInfo reads the Info held on the handle's cached bucket. It does not use
// Bucket.Attrs, which refreshes the cache from the server first.
func cachedInfo(t *testing.T, bucket *Bucket) map[string]string {
	t.Helper()
	be, ok := bucket.b.(*beBucket)
	if !ok {
		t.Fatalf("bucket backend is %T, want *beBucket", bucket.b)
	}
	bb, ok := be.b2bucket.(*b2Bucket)
	if !ok {
		t.Fatalf("bucket baseline is %T, want *b2Bucket", be.b2bucket)
	}
	return bb.b.Info
}

// A failed update must leave the cached Info as it was, whether the caller
// cleared it, replaced it, or left it nil.
func TestFailedBucketUpdateRestoresCachedInfo(t *testing.T) {
	tests := []struct {
		name string
		info map[string]string
		typ  BucketType
	}{
		{name: "empty map (clear)", info: map[string]string{}},
		{name: "replacement map", info: map[string]string{"new": "value"}},
		{name: "nil with another change", typ: Public},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bucketJSON := map[string]interface{}{
				"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
				"bucketInfo": map[string]string{"old": "value"}, "revision": 1,
				"fileLockConfiguration":    map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
				"replicationConfiguration": map[string]interface{}{"value": nil},
			}
			transport := bucketInfoRoundTripper(func(req *http.Request) (*http.Response, error) {
				var resp interface{}
				status := http.StatusOK
				switch {
				case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
					resp = map[string]interface{}{
						"accountId": "account", "authorizationToken": "token",
						"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
							"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
						}},
					}
				case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
					resp = map[string]interface{}{"buckets": []interface{}{bucketJSON}}
				case strings.HasSuffix(req.URL.Path, "/b2_update_bucket"):
					status = http.StatusBadRequest
					resp = map[string]interface{}{"status": 400, "code": "bad_request", "message": "rejected"}
				default:
					t.Errorf("unexpected request %s", req.URL)
				}
				body, err := json.Marshal(resp)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
			})
			ctx := context.Background()
			client, err := NewClient(ctx, "account", "key", APIBase("https://b2.test"), Transport(transport))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.Bucket(ctx, "bucket")
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"old": "value"}
			if got := cachedInfo(t, bucket); !reflect.DeepEqual(got, want) {
				t.Fatalf("setup: cached Info = %v, want %v", got, want)
			}

			if err := bucket.Update(ctx, &BucketAttrs{Type: tc.typ, Info: tc.info}); err == nil {
				t.Fatal("Update succeeded, want the rejected request to fail")
			}
			if got := cachedInfo(t, bucket); !reflect.DeepEqual(got, want) {
				t.Errorf("cached Info after a failed update = %v, want the previous %v", got, want)
			}
		})
	}
}
