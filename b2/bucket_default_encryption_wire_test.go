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
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const sseB2DefaultJSON = `"defaultServerSideEncryption":{"isClientAuthorizedToRead":true,"value":{"algorithm":"AES256","mode":"SSE-B2"}},`

func bucketJSONWithEncryption() string {
	return strings.Replace(updateBucketJSON, `"revision":2,`, `"revision":2,`+sseB2DefaultJSON, 1)
}

func TestRegression_UnrelatedUpdateDoesNotSendCachedDefaultEncryption(t *testing.T) {
	srv, bucket := bucketListedWith(t, bucketJSONWithEncryption())
	if err := bucket.Update(newTestCtx(t), &BucketAttrs{Info: map[string]string{"k": "v"}}); err != nil {
		t.Fatal(err)
	}
	if v, ok := srv.Last(t, "b2_update_bucket").JSON["defaultServerSideEncryption"]; ok {
		t.Errorf("info-only update re-sent the cached defaultServerSideEncryption=%v", v)
	}
}

func TestBucketUpdateSetsEncryptionThenOmitsIt(t *testing.T) {
	srv, bucket := bucketListedWith(t, updateBucketJSON)
	srv.Reply("b2_update_bucket", 200, bucketJSONWithEncryption())
	ctx := newTestCtx(t)
	if err := bucket.Update(ctx, &BucketAttrs{DefaultServerSideEncryption: SSEB2WithAES256()}); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"k": "v"}}); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("b2_update_bucket")
	if len(calls) != 2 {
		t.Fatalf("update calls = %d, want 2", len(calls))
	}
	b2wiretest.AssertJSON(t, calls[0], "defaultServerSideEncryption.mode", "SSE-B2")
	b2wiretest.AssertJSON(t, calls[0], "defaultServerSideEncryption.algorithm", "AES256")
	b2wiretest.AssertJSON(t, calls[1], "bucketInfo.k", "v")
	b2wiretest.AssertNoJSON(t, calls[1], "defaultServerSideEncryption")
}

// Reading attrs and updating another field passes the cached encryption back
// in. The caller did not change it, so the request must not carry it.
func TestBucketUpdateOmitsUnchangedEncryptionReadFromAttrs(t *testing.T) {
	srv, bucket := bucketListedWith(t, bucketJSONWithEncryption())
	ctx := newTestCtx(t)
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.DefaultServerSideEncryption == nil {
		t.Fatal("setup: Attrs did not report the default encryption")
	}
	attrs.Info = map[string]string{"k": "v"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatal(err)
	}
	call := srv.Last(t, "b2_update_bucket")
	b2wiretest.AssertJSON(t, call, "bucketInfo.k", "v")
	b2wiretest.AssertNoJSON(t, call, "defaultServerSideEncryption")
}
