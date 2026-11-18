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
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// bucketListedWith returns a client and the bucket "n" as listed by the server
// (so its cached attributes come from listJSON), with b2_update_bucket scripted
// by the caller afterwards.
func bucketListedWith(t *testing.T, listJSON string) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+listJSON+`]}`)
	srv.Reply("b2_update_bucket", http.StatusOK, listJSON)
	ctx := newTestCtx(t)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

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

// Existing CORS rules can be cleared with an empty list.
func TestKnownDefect_EmptyCORSListClearsRules(t *testing.T) {
	const cors = `"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"]}],`
	srv, bucket := bucketListedWith(t, bucketJSONWith(cors))
	if err := bucket.Update(newTestCtx(t), &BucketAttrs{CORSRules: []CORSRule{}}); err != nil {
		t.Fatal(err)
	}
	got, ok := srv.Last(t, "b2_update_bucket").JSON["corsRules"].([]any)
	if !ok || len(got) != 0 {
		t.Errorf("update with an empty CORS list sent corsRules=%v, want an empty list", srv.Last(t, "b2_update_bucket").JSON["corsRules"])
	}
}

// NewBucket must not silently ignore a requested default
// retention: it is either applied (create or follow-up update) or rejected.
func TestKnownDefect_NewBucketDoesNotIgnoreDefaultRetention(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, updateBucketJSON)
	srv.Reply("b2_update_bucket", http.StatusOK, updateBucketJSON)
	ctx := newTestCtx(t)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.NewBucket(ctx, "n", &BucketAttrs{
		Type:             Private,
		FileLockEnabled:  true,
		DefaultRetention: &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 7, Unit: "days"}},
	})
	if err != nil {
		return // rejecting the unsupported request is acceptable
	}
	for _, call := range []string{"b2_create_bucket", "b2_update_bucket"} {
		for _, r := range srv.Calls(call) {
			if _, ok := r.JSON["defaultRetention"]; ok {
				return
			}
		}
	}
	t.Error("NewBucket returned nil error but no request carried the requested defaultRetention")
}

// Reveal must report a listing error, not "not found".
func TestKnownDefect_RevealReportsListingError(t *testing.T) {
	srv, bucket := bucketListedWith(t, updateBucketJSON)
	forbidden := b2wiretest.Response{Status: http.StatusForbidden, Body: `{"status":403,"code":"access_denied","message":"no listFiles capability"}`}
	srv.ReplySequence("b2_list_file_names", forbidden)
	srv.ReplySequence("b2_list_file_versions", forbidden)
	err := bucket.Reveal(newTestCtx(t), "obj")
	if err == nil {
		t.Fatal("Reveal returned nil although the listing was refused")
	}
	if IsNotExist(err) {
		t.Errorf("a refused listing was reported as not-found: %v", err)
	}
}

// Listing unfinished uploads with a name prefix must only return
// uploads that match the prefix.
func TestKnownDefect_ListUnfinishedHonoursPrefix(t *testing.T) {
	srv, bucket := bucketListedWith(t, updateBucketJSON)
	all := []string{"logs/a", "other/b", "logs/c", "tmp/d"}
	srv.Handle("b2_list_unfinished_large_files", func(r b2wiretest.Request) b2wiretest.Response {
		prefix, _ := r.JSON["namePrefix"].(string) // a server honours the B2 namePrefix parameter
		var files []string
		for _, n := range all {
			if strings.HasPrefix(n, prefix) {
				files = append(files, `{"fileName":"`+n+`","fileId":"id-`+n+`","action":"start"}`)
			}
		}
		return b2wiretest.Response{Body: `{"files":[` + strings.Join(files, ",") + `],"nextFileId":null}`}
	})
	iter := bucket.List(newTestCtx(t), ListUnfinished(), ListPrefix("logs/"))
	var got []string
	for iter.Next() {
		got = append(got, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "logs/") {
			t.Errorf("ListUnfinished with ListPrefix(\"logs/\") returned %q", n)
		}
	}
}

// ListKeys must not treat an empty page that still has a
// next-page marker as the end of the list.
func TestKnownDefect_ListKeysContinuesPastEmptyPageWithCursor(t *testing.T) {
	srv, bucket := bucketListedWith(t, updateBucketJSON)
	srv.Reply("b2_list_keys", http.StatusOK, `{"keys":[],"nextApplicationKeyId":"next-id"}`)
	keys, next, err := bucket.c.ListKeys(newTestCtx(t), 10, "")
	if errors.Is(err, io.EOF) || next != "next-id" {
		t.Errorf("ListKeys = (%d keys, next=%q, err=%v), want the cursor %q and no EOF", len(keys), next, err, "next-id")
	}
}

// Deleting a key after the sign-in token expired must sign in
// again and succeed, like every other call.
func TestKnownDefect_DeleteKeyReauthorizesAfterTokenExpiry(t *testing.T) {
	srv, bucket := bucketListedWith(t, updateBucketJSON)
	srv.Reply("b2_list_keys", http.StatusOK, `{"keys":[{"accountId":"a","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}],"nextApplicationKeyId":null}`)
	ctx := newTestCtx(t)
	keys, _, err := bucket.c.ListKeys(ctx, 10, "")
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("listed %d keys, want 1", len(keys))
	}
	srv.ReplySequence("b2_delete_key",
		b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`},
		b2wiretest.Response{Body: `{"accountId":"a","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}`})
	authBefore := len(srv.Calls("b2_authorize_account"))
	if err := keys[0].Delete(ctx); err != nil {
		t.Errorf("Delete after token expiry failed instead of re-authorizing: %v", err)
	}
	if got := len(srv.Calls("b2_authorize_account")) - authBefore; got != 1 {
		t.Errorf("%d re-authorizations after the 401, want 1", got)
	}
}

// Calls that must not be repeated blindly (create key, create
// bucket, start large file, finish large file) are sent once after a gateway error.
func TestKnownDefect_NonIdempotentCallsAreNotResentAfter504(t *testing.T) {
	gateway := b2wiretest.Response{Status: http.StatusGatewayTimeout, Body: `{"status":504,"code":"gateway_timeout","message":"timeout"}`}
	for _, tc := range []struct {
		name, call string
		ok         b2wiretest.Response
		run        func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket)
	}{
		{"create key", "b2_create_key",
			b2wiretest.Response{Body: `{"accountId":"a","applicationKey":"s","applicationKeyId":"kid","bucketId":null,"capabilities":["listFiles"],"expirationTimestamp":null,"keyName":"k","namePrefix":null,"options":[]}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) {
				_, _ = bucket.c.CreateKey(newTestCtx(t), "k", Capabilities("listFiles"))
			}},
		{"create bucket", "b2_create_bucket", b2wiretest.Response{Body: updateBucketJSON},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) {
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
				_, _ = bucket.c.NewBucket(newTestCtx(t), "fresh", &BucketAttrs{Type: Private})
			}},
		{"start large file", "b2_start_large_file", b2wiretest.Response{Body: `{"fileId":"lid","fileName":"o"}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) {
				srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"lid","fileName":"o","action":"upload"}`)
				w := bucket.Object("o").NewWriter(newTestCtx(t))
				w.ChunkSize = 10
				_, _ = w.Write(make([]byte, 25))
				_ = w.Close()
			}},
		{"finish large file", "b2_finish_large_file", b2wiretest.Response{Body: `{"fileId":"lid","fileName":"o","action":"upload"}`},
			func(t *testing.T, srv *b2wiretest.Server, bucket *Bucket) {
				srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"lid","fileName":"o"}`)
				w := bucket.Object("o").NewWriter(newTestCtx(t))
				w.ChunkSize = 10
				_, _ = w.Write(make([]byte, 25))
				_ = w.Close()
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := bucketListedWith(t, updateBucketJSON)
			srv.ReplySequence(tc.call, gateway, tc.ok)
			tc.run(t, srv, bucket)
			if got := len(srv.Calls(tc.call)); got != 1 {
				t.Errorf("%s sent %d times after a 504 reply, want 1", tc.call, got)
			}
		})
	}
}

// Library errors must unwrap, so errors.Is(err, fs.ErrNotExist)
// works for not-found errors (as the documented b2.IsNotExist does).
func TestKnownDefect_NotFoundErrorUnwrapsToFSErrNotExist(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	ctx := newTestCtx(t)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Bucket(ctx, "missing")
	if err == nil || !IsNotExist(err) {
		t.Fatalf("setup: Bucket(missing) err = %v, want a not-found error", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(err, fs.ErrNotExist) = false for %v; library errors do not unwrap", err)
	}
}
