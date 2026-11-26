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
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// A rejected update must not leak its settings into the next update.
func TestRegression_RejectedUpdateDoesNotLeakIntoNextUpdate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		attrs *BucketAttrs
		key   string
	}{
		{"visibility", &BucketAttrs{Type: Public}, "bucketType"},
		{"CORS rules", &BucketAttrs{CORSRules: []CORSRule{{Name: "r", AllowedOrigins: []string{"https://example.com"}, AllowedOperations: []string{"b2_download_file_by_name"}}}}, "corsRules"},
		{"default encryption", &BucketAttrs{DefaultServerSideEncryption: &ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"}}, "defaultServerSideEncryption"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := bucketListedWith(t, updateBucketJSON)
			srv.ReplySequence("b2_update_bucket", b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"rejected"}`}, b2wiretest.Response{Body: updateBucketJSON})
			ctx := newTestCtx(t)
			if err := bucket.Update(ctx, tc.attrs); err == nil {
				t.Fatal("first update: expected a rejection")
			}
			if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"k": "v"}}); err != nil {
				t.Fatalf("second update: %v", err)
			}
			calls := srv.Calls("b2_update_bucket")
			if len(calls) != 2 {
				t.Fatalf("b2_update_bucket calls = %d, want 2", len(calls))
			}
			if v, ok := calls[1].JSON[tc.key]; ok {
				// Visibility may carry the original type; it must not carry the rejected type.
				if tc.key != "bucketType" || v != "allPrivate" {
					t.Errorf("info-only update after a rejected %s update sent %s=%v", tc.name, tc.key, v)
				}
			}
			if tc.name == "visibility" {
				b2wiretest.AssertJSON(t, calls[0], "bucketType", "allPublic")
				b2wiretest.AssertJSON(t, calls[1], "bucketInfo.k", "v")
				if v, ok := calls[1].JSON["bucketType"]; ok && v == "allPublic" {
					t.Errorf("second update sent rejected bucketType=%v", v)
				}
			}
		})
	}
}
