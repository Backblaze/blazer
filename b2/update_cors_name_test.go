package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
)

const updateAuthJSON = `{"accountId":"a","authorizationToken":"t","applicationKeyExpirationTimestamp":0,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"apiUrl":"%[1]s","capabilities":[],"downloadUrl":"%[1]s","storageApi":"storage","recommendedPartSize":100000000,"s3ApiUrl":"%[1]s","allowed":{"buckets":null,"capabilities":[],"namePrefix":null}}}}`

const updateBucketJSON = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
	`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},` +
	`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`

func TestUpdateBucketSendsCORSRuleName(t *testing.T) {
	var srv *httptest.Server
	var sent map[string]any
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			fmt.Fprint(w, updateBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			fmt.Fprint(w, updateBucketJSON)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	root, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	err = (&b2Bucket{b: bb}).updateBucket(ctx, &BucketAttrs{CORSRules: []CORSRule{{
		Name:              "browser",
		AllowedOrigins:    []string{"https://example.com"},
		AllowedOperations: []string{"b2_download_file_by_name"},
	}}})
	if err != nil {
		t.Fatalf("updateBucket: %v", err)
	}
	rules, ok := sent["corsRules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("update request corsRules = %v, want one rule", sent["corsRules"])
	}
	if name := rules[0].(map[string]any)["corsRuleName"]; name != "browser" {
		t.Errorf("update request corsRuleName = %v, want browser", name)
	}
}

func TestReadModifyWriteKeepsCORSRuleName(t *testing.T) {
	const withCORS = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
		`"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"]}],` +
		`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},` +
		`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`
	var srv *httptest.Server
	var sent map[string]any
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			fmt.Fprint(w, withCORS)
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			fmt.Fprint(w, withCORS)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	root, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false)
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
	rules, ok := sent["corsRules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("update request corsRules = %v, want the existing rule", sent["corsRules"])
	}
	if name := rules[0].(map[string]any)["corsRuleName"]; name != "browser" {
		t.Errorf("update request corsRuleName = %v, want browser", name)
	}
}
