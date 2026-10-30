package b2

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Literal b2_create_bucket response, independent of the SDK's JSON structs.
// https://www.backblaze.com/apidocs/b2-create-bucket
const attrsWireBucketJSON = `{"bucketId":"bucket-id","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1,"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedHeaders":["authorization"],"allowedOperations":["b2_download_file_by_name"],"exposeHeaders":["x-bz-file-name"],"maxAgeSeconds":60}],"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":"governance","period":{"duration":7,"unit":"days"}}}}}`

func attrsWireBucket(t *testing.T) *b2Bucket {
	t.Helper()
	wire := b2wiretest.New(t)
	wire.Reply("b2_create_bucket", http.StatusOK, attrsWireBucketJSON)
	root, err := base.AuthorizeAccount(context.Background(), "account", "key", base.SetAPIBase(wire.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := root.CreateBucket(context.Background(), "bucket", "allPrivate", nil, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &b2Bucket{b: bucket}
}

func TestBucketAttrsCORSAndFileLock(t *testing.T) {
	wantCORS := []CORSRule{{
		Name:              "browser",
		AllowedOrigins:    []string{"https://example.com"},
		AllowedHeaders:    []string{"authorization"},
		AllowedOperations: []string{"b2_download_file_by_name"},
		ExposeHeaders:     []string{"x-bz-file-name"},
		MaxAgeSeconds:     60,
	}}
	bucket := attrsWireBucket(t)

	attrs := bucket.attrs()
	if !reflect.DeepEqual(attrs.CORSRules, wantCORS) {
		t.Errorf("attrs().CORSRules = %#v, want %#v", attrs.CORSRules, wantCORS)
	}
	if !attrs.FileLockEnabled {
		t.Error("attrs().FileLockEnabled = false, want true")
	}
}
