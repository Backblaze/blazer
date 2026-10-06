// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

const corsAndLockBucketJSON = `{
	"bucketId": "bid",
	"bucketName": "name",
	"bucketType": "allPrivate",
	"bucketInfo": {},
	"lifecycleRules": [],
	"revision": 1,
	"corsRules": [{"corsRuleName": "browser", "allowedOrigins": ["https://example.com"], "allowedOperations": ["b2_download_file_by_name"], "maxAgeSeconds": 60}],
	"fileLockConfiguration": {"isClientAuthorizedToRead": true, "value": {"isFileLockEnabled": true, "defaultRetention": {"mode": null, "period": null}}}
}`

const plainBucketJSON = `{"bucketId": "bid", "bucketName": "name", "bucketType": "allPrivate", "bucketInfo": {}, "lifecycleRules": [], "revision": 1}`

// bucketFixture serves authorize_account, then answers b2_create_bucket and
// b2_list_buckets with bucketJSON and records the create request body.
func bucketFixture(t *testing.T, bucketJSON string) (*B2, *map[string]any, func()) {
	t.Helper()
	var srv *httptest.Server
	createBody := map[string]any{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			if err := json.Unmarshal(body, &createBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			fmt.Fprint(w, bucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			fmt.Fprintf(w, `{"buckets": [%s]}`, bucketJSON)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		srv.Close()
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	return b, &createBody, srv.Close
}

func TestCreateBucketSendsAndReadsCORSAndFileLock(t *testing.T) {
	b, sent, closeSrv := bucketFixture(t, corsAndLockBucketJSON)
	defer closeSrv()

	rules := []b2types.CORSRule{{
		Name:              "browser",
		AllowedOrigins:    []string{"https://example.com"},
		AllowedOperations: []string{"b2_download_file_by_name"},
		MaxAgeSeconds:     60,
	}}
	bucket, err := b.CreateBucket(context.Background(), "name", "allPrivate", nil, nil, nil, rules, true)
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	if got, ok := (*sent)["fileLockEnabled"].(bool); !ok || !got {
		t.Errorf("request fileLockEnabled = %v, want true", (*sent)["fileLockEnabled"])
	}
	gotRules, ok := (*sent)["corsRules"].([]any)
	if !ok || len(gotRules) != 1 {
		t.Fatalf("request corsRules = %v, want one rule", (*sent)["corsRules"])
	}
	if name := gotRules[0].(map[string]any)["corsRuleName"]; name != "browser" {
		t.Errorf("request corsRuleName = %v, want browser", name)
	}

	if !bucket.FileLockEnabled {
		t.Error("bucket.FileLockEnabled = false, want true from the response")
	}
	if !reflect.DeepEqual(bucket.CORSRules, rules) {
		t.Errorf("bucket.CORSRules = %#v, want %#v", bucket.CORSRules, rules)
	}
}

func TestCreateBucketOmitsUnsetCORSAndFileLock(t *testing.T) {
	b, sent, closeSrv := bucketFixture(t, plainBucketJSON)
	defer closeSrv()

	bucket, err := b.CreateBucket(context.Background(), "name", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	for _, k := range []string{"corsRules", "fileLockEnabled"} {
		if v, ok := (*sent)[k]; ok {
			t.Errorf("request unexpectedly contained %s: %v", k, v)
		}
	}
	if bucket.FileLockEnabled || len(bucket.CORSRules) != 0 {
		t.Errorf("bucket = %+v, want no CORS and no file lock", bucket)
	}
}

func TestListBucketsReadsCORSAndFileLock(t *testing.T) {
	b, _, closeSrv := bucketFixture(t, corsAndLockBucketJSON)
	defer closeSrv()

	buckets, err := b.ListBuckets(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("got %d buckets, want 1", len(buckets))
	}
	if !buckets[0].FileLockEnabled {
		t.Error("FileLockEnabled = false, want true")
	}
	if len(buckets[0].CORSRules) != 1 || buckets[0].CORSRules[0].Name != "browser" {
		t.Errorf("CORSRules = %#v, want the browser rule", buckets[0].CORSRules)
	}
}

func TestListBucketsWithoutFileLockConfiguration(t *testing.T) {
	b, _, closeSrv := bucketFixture(t, plainBucketJSON)
	defer closeSrv()

	buckets, err := b.ListBuckets(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].FileLockEnabled {
		t.Errorf("buckets = %+v, want one bucket without file lock", buckets)
	}
}
