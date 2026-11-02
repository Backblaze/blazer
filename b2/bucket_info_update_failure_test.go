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

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2types"
)

// cachedBucket returns the base.Bucket held on the handle's cached bucket. It
// does not use Bucket.Attrs, which refreshes the cache from the server first.
func cachedBucket(t *testing.T, bucket *Bucket) *base.Bucket {
	t.Helper()
	be, ok := bucket.b.(*beBucket)
	if !ok {
		t.Fatalf("bucket backend is %T, want *beBucket", bucket.b)
	}
	bb, ok := be.b2bucket.(*b2Bucket)
	if !ok {
		t.Fatalf("bucket baseline is %T, want *b2Bucket", be.b2bucket)
	}
	return bb.b
}

// cachedInfo reads the Info held on the handle's cached bucket.
func cachedInfo(t *testing.T, bucket *Bucket) map[string]string {
	t.Helper()
	return cachedBucket(t, bucket).Info
}

// newBucketRejectingUpdates returns a handle for a bucket described by
// bucketJSON (one b2_list_buckets entry) whose b2_update_bucket calls all fail
// with a 400.
func newBucketRejectingUpdates(t *testing.T, bucketJSON map[string]interface{}) *Bucket {
	t.Helper()
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
	return bucket
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
			bucket := newBucketRejectingUpdates(t, bucketJSON)
			ctx := context.Background()
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

// KNOWN BUG, tracked upstream as Backblaze/blazer#108: this test FAILS on main
// until that issue is fixed, and passing it is the acceptance check for the fix.
//
// b2Bucket.updateBucket copies the caller's attributes onto the cached
// base.Bucket before sending the request and, when the request fails, restores
// only Info, LifecycleRules, DefaultRetention, FileLockEnabled and
// ReplicationConfiguration. Type, CORSRules and DefaultServerSideEncryption
// stay modified, so the next Update re-sends them (a failed "make public" can
// take effect on a later, unrelated update).
func TestKnownBug108FailedBucketUpdateRestoresTypeCORSAndEncryption(t *testing.T) {
	oldCORS := []b2types.CORSRule{{
		Name:              "old-rule",
		AllowedOrigins:    []string{"https://old.example"},
		AllowedOperations: []string{"b2_download_file_by_name"},
	}}
	bucketJSON := map[string]interface{}{
		"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
		"bucketInfo": map[string]string{}, "revision": 1,
		"corsRules": oldCORS,
		"defaultServerSideEncryption": map[string]interface{}{
			"isClientAuthorizedToRead": true,
			"value":                    map[string]interface{}{"mode": nil, "algorithm": nil},
		},
		"fileLockConfiguration":    map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
		"replicationConfiguration": map[string]interface{}{"value": nil},
	}
	bucket := newBucketRejectingUpdates(t, bucketJSON)

	before := cachedBucket(t, bucket)
	if before.Type != "allPrivate" || !reflect.DeepEqual(before.CORSRules, oldCORS) {
		t.Fatalf("setup: cached Type=%q CORSRules=%v, want allPrivate and %v", before.Type, before.CORSRules, oldCORS)
	}
	wantSSE := before.DefaultServerSideEncryption
	wantCORS := append([]b2types.CORSRule(nil), before.CORSRules...)

	err := bucket.Update(context.Background(), &BucketAttrs{
		Type: Public,
		CORSRules: []CORSRule{{
			Name:              "new-rule",
			AllowedOrigins:    []string{"*"},
			AllowedOperations: []string{"b2_download_file_by_name"},
		}},
		DefaultServerSideEncryption: &ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"},
	})
	if err == nil {
		t.Fatal("Update succeeded, want the rejected request to fail")
	}

	after := cachedBucket(t, bucket)
	t.Run("Type", func(t *testing.T) {
		if after.Type != "allPrivate" {
			t.Errorf("cached Type after a failed update = %q, want the previous %q (Backblaze/blazer#108)", after.Type, "allPrivate")
		}
	})
	t.Run("CORSRules", func(t *testing.T) {
		if !reflect.DeepEqual(after.CORSRules, wantCORS) {
			t.Errorf("cached CORSRules after a failed update = %v, want the previous %v (Backblaze/blazer#108)", after.CORSRules, wantCORS)
		}
	})
	t.Run("DefaultServerSideEncryption", func(t *testing.T) {
		if !reflect.DeepEqual(after.DefaultServerSideEncryption, wantSSE) {
			t.Errorf("cached DefaultServerSideEncryption after a failed update = %+v, want the previous %+v (Backblaze/blazer#108)", after.DefaultServerSideEncryption, wantSSE)
		}
	})
}
