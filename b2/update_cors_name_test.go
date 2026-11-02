package b2

import (
	"context"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const updateAuthJSON = `{"accountId":"a","authorizationToken":"t","applicationKeyExpirationTimestamp":0,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"apiUrl":"%[1]s","capabilities":[],"downloadUrl":"%[1]s","storageApi":"storage","recommendedPartSize":100000000,"s3ApiUrl":"%[1]s","allowed":{"buckets":null,"capabilities":[],"namePrefix":null}}}}`

const updateBucketJSON = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
	`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},` +
	`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`

func TestUpdateBucketSendsCORSRuleName(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_create_bucket", http.StatusOK, updateBucketJSON)
	srv.Reply("b2_update_bucket", http.StatusOK, updateBucketJSON)

	ctx := context.Background()
	root, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = (&b2Bucket{b: bb}).updateBucket(ctx, &BucketAttrs{CORSRules: []CORSRule{{
		Name:              "browser",
		AllowedOrigins:    []string{"https://example.com"},
		AllowedOperations: []string{"b2_download_file_by_name"},
		MaxAgeSeconds:     60,
	}}})
	if err != nil {
		t.Fatalf("updateBucket: %v", err)
	}
	request := srv.Last(t, "b2_update_bucket")
	b2wiretest.AssertJSON(t, request, "corsRules", []map[string]any{{
		"corsRuleName":      "browser",
		"allowedOrigins":    []string{"https://example.com"},
		"allowedOperations": []string{"b2_download_file_by_name"},
		"maxAgeSeconds":     60,
	}})
}

func TestReadModifyWriteKeepsCORSRuleName(t *testing.T) {
	const withCORS = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
		`"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"],"maxAgeSeconds":60}],` +
		`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},` +
		`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`
	srv := b2wiretest.New(t)
	srv.Reply("b2_create_bucket", http.StatusOK, withCORS)
	srv.Reply("b2_update_bucket", http.StatusOK, withCORS)

	ctx := context.Background()
	root, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	bucket := &b2Bucket{b: bb}

	// Attrs() then Update(attrs), with the unrelated Info edited in between.
	attrs := bucket.attrs()
	attrs.Info = map[string]string{"edited": "yes"}
	if err := bucket.updateBucket(ctx, attrs); err != nil {
		t.Fatalf("updateBucket: %v", err)
	}
	b2wiretest.AssertJSON(t, srv.Last(t, "b2_update_bucket"), "corsRules", []map[string]any{{
		"corsRuleName": "browser", "allowedOrigins": []string{"https://example.com"},
		"allowedOperations": []string{"b2_download_file_by_name"}, "maxAgeSeconds": 60,
	}})
}
