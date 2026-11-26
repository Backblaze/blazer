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

//go:build knowndefect

package b2

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func bucketJSONWith(extra string) string {
	return strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,`+extra, 1)
}

const sseB2DefaultJSON = `"defaultServerSideEncryption":{"isClientAuthorizedToRead":true,"value":{"algorithm":"AES256","mode":"SSE-B2"}},`

var errBadRequest = b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"rejected"}`}

// After a rejected bucket update, the cached bucket must not keep
// the rejected setting; the next unrelated update must not re-send it.
func TestKnownDefect_RejectedUpdateDoesNotLeakIntoNextUpdate(t *testing.T) {
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
			srv.ReplySequence("b2_update_bucket", errBadRequest, b2wiretest.Response{Body: updateBucketJSON})
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
				t.Errorf("info-only update after a rejected %s update sent %s=%v", tc.name, tc.key, v)
			}
		})
	}
}

// An existing default encryption setting must not be re-sent with
// every unrelated bucket update (a key lacking the encryption permission fails).
func TestKnownDefect_UnrelatedUpdateDoesNotSendCachedDefaultEncryption(t *testing.T) {
	srv, bucket := bucketListedWith(t, bucketJSONWith(sseB2DefaultJSON))
	if err := bucket.Update(newTestCtx(t), &BucketAttrs{Info: map[string]string{"k": "v"}}); err != nil {
		t.Fatal(err)
	}
	if v, ok := srv.Last(t, "b2_update_bucket").JSON["defaultServerSideEncryption"]; ok {
		t.Errorf("info-only update re-sent the cached defaultServerSideEncryption=%v", v)
	}
}
