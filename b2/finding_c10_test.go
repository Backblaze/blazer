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

package b2

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// https://www.backblaze.com/apidocs/b2-update-bucket
func TestFindingC10_EmptyCORSListClearsRules(t *testing.T) {
	const cors = `"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"]}],`
	withCORS := strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,`+cors, 1)
	cleared := strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,"corsRules":[],`, 1)
	bucket, srv := newMappingBucket(t, withCORS)
	var mu sync.Mutex
	current := withCORS
	srv.Handle("b2_update_bucket", func(req b2wiretest.Request) b2wiretest.Response {
		mu.Lock()
		defer mu.Unlock()
		if rules, ok := req.JSON["corsRules"].([]any); ok && len(rules) == 0 {
			current = cleared
		}
		return b2wiretest.Response{Status: http.StatusOK, Body: current}
	})
	srv.Handle("b2_list_buckets", func(b2wiretest.Request) b2wiretest.Response {
		mu.Lock()
		defer mu.Unlock()
		return b2wiretest.Response{Status: http.StatusOK, Body: `{"buckets":[` + current + `]}`}
	})
	ctx := context.Background()
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"updated": "yes"}}); err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil || len(attrs.CORSRules) != 1 || attrs.CORSRules[0].Name != "browser" {
		t.Fatalf("unset CORS: attrs=%+v, err=%v; want the existing browser rule", attrs, err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{CORSRules: []CORSRule{}}); err != nil {
		t.Fatal(err)
	}
	request := srv.Last(t, "b2_update_bucket")
	if rules, ok := request.JSON["corsRules"].([]any); !ok || len(rules) != 0 {
		t.Errorf("empty CORS update sent corsRules=%v, want []", request.JSON["corsRules"])
	}
	attrs, err = bucket.Attrs(ctx)
	if err != nil || len(attrs.CORSRules) != 0 {
		t.Errorf("after clearing CORS: attrs=%+v, err=%v; want no rules", attrs, err)
	}
}

func TestFindingC10_UnsetCORSAfterClearIsOmitted(t *testing.T) {
	const empty = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","corsRules":[],"revision":2}`
	bucket, srv := newMappingBucket(t, empty)
	if err := bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"updated": "yes"}}); err != nil {
		t.Fatal(err)
	}
	if value, ok := srv.Last(t, "b2_update_bucket").JSON["corsRules"]; ok {
		t.Errorf("unset CORS after an empty response sent corsRules=%v, want it omitted", value)
	}
}

func TestFindingC10_UnsetCachedCORSIsOmitted(t *testing.T) {
	const cors = `"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"]}],`
	response := strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,`+cors, 1)
	bucket, srv := newMappingBucket(t, response)
	if err := bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"updated": "yes"}}); err != nil {
		t.Fatal(err)
	}
	if value, ok := srv.Last(t, "b2_update_bucket").JSON["corsRules"]; ok {
		t.Errorf("unset CORS sent cached corsRules=%v, want it omitted", value)
	}
}
