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
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// A rejected Update must not leave its Type, CORS rules or default encryption
// on the cached bucket: the next Update on the same handle sends the cached
// values, and B2 would apply the change that was rejected.
// https://www.backblaze.com/apidocs/b2-update-bucket
func TestFailedBucketUpdateDoesNotLeakIntoNextUpdate(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.ReplySequence("b2_update_bucket",
		b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"rejected"}`},
		b2wiretest.Response{Status: http.StatusOK, Body: updateBucketJSON},
	)
	ctx := context.Background()
	err := bucket.Update(ctx, &BucketAttrs{
		Type:                        Public,
		CORSRules:                   []CORSRule{{Name: "browser", AllowedOrigins: []string{"https://example.com"}, AllowedOperations: []string{"b2_download_file_by_name"}}},
		DefaultServerSideEncryption: SSEB2WithAES256(),
	})
	if err == nil {
		t.Fatal("Update succeeded, want the rejected request to fail")
	}
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"k": "v"}}); err != nil {
		t.Fatal(err)
	}
	updates := srv.Calls("b2_update_bucket")
	if len(updates) != 2 {
		t.Fatalf("b2_update_bucket calls = %d, want 2", len(updates))
	}
	second := updates[1]
	if v, ok := second.JSON["bucketType"]; ok && v == "allPublic" {
		t.Errorf("second update sent bucketType %v from the rejected first update", v)
	}
	b2wiretest.AssertNoJSON(t, second, "corsRules")
	b2wiretest.AssertNoJSON(t, second, "defaultServerSideEncryption")
}
