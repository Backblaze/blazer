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
	"strings"
	"testing"
)

func bucketJSONWith(extra string) string {
	return strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,`+extra, 1)
}

const sseB2DefaultJSON = `"defaultServerSideEncryption":{"isClientAuthorizedToRead":true,"value":{"algorithm":"AES256","mode":"SSE-B2"}},`

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
