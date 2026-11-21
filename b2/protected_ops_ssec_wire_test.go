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
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestProtectedObjectOperationsWithKey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		call   string
		run    func(context.Context, *Object, *SSECKey) error
		bypass bool
	}{
		{"delete", "b2_delete_file_version", func(ctx context.Context, o *Object, key *SSECKey) error { return o.Delete(ctx, WithDeleteSSECKey(key)) }, false},
		{"delete with governance bypass", "b2_delete_file_version", func(ctx context.Context, o *Object, key *SSECKey) error {
			return o.Delete(ctx, WithDeleteSSECKey(key), BypassGovernance())
		}, true},
		{"hide", "b2_hide_file", func(ctx context.Context, o *Object, key *SSECKey) error { return o.Hide(ctx, key) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			key, err := NewSSECKey([]byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			srv.Handle("protected", func(r b2wiretest.Request) b2wiretest.Response {
				if r.Method != http.MethodHead ||
					r.Header.Get("X-Bz-Server-Side-Encryption-Customer-Algorithm") != "AES256" ||
					r.Header.Get("X-Bz-Server-Side-Encryption-Customer-Key") != "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" ||
					r.Header.Get("X-Bz-Server-Side-Encryption-Customer-Key-Md5") != "hRasmdxgYDKV3nvbahU1MA==" {
					return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"missing or invalid customer key"}`}
				}
				return b2wiretest.Response{Header: http.Header{"Content-Length": {"0"}, "X-Bz-File-Id": {"file-id"}, "X-Bz-File-Name": {"protected"}}}
			})
			srv.Reply(tc.call, http.StatusOK, `{"fileId":"file-id","fileName":"protected","action":"hide"}`)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tc.run(ctx, bucket.Object("protected"), key); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			heads := srv.Calls("protected")
			if len(heads) != 1 || heads[0].Method != http.MethodHead {
				t.Fatalf("HEAD calls = %d, want one HEAD", len(heads))
			}
			calls := srv.Calls(tc.call)
			if len(calls) != 1 {
				t.Fatalf("%s calls = %d, want 1", tc.call, len(calls))
			}
			b2wiretest.AssertJSON(t, calls[0], "fileName", "protected")
			if tc.call == "b2_delete_file_version" {
				b2wiretest.AssertJSON(t, calls[0], "fileId", "file-id")
				if tc.bypass {
					b2wiretest.AssertJSON(t, calls[0], "bypassGovernance", true)
				} else {
					b2wiretest.AssertNoJSON(t, calls[0], "bypassGovernance")
				}
			} else {
				b2wiretest.AssertJSON(t, calls[0], "bucketId", "bid")
			}
			assertNoSSECWireHeaders(t, calls[0].Header)
		})
	}
}

func TestProtectedObjectOperationsRejectInvalidKeyBeforeRequest(t *testing.T) {
	key, err := NewSSECKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		run  func(context.Context, *Object) error
	}{
		{"delete nil", func(ctx context.Context, o *Object) error { return o.Delete(ctx, WithDeleteSSECKey(nil)) }},
		{"delete zero", func(ctx context.Context, o *Object) error { return o.Delete(ctx, WithDeleteSSECKey(&SSECKey{})) }},
		{"delete multiple", func(ctx context.Context, o *Object) error {
			return o.Delete(ctx, WithDeleteSSECKey(key), WithDeleteSSECKey(key))
		}},
		{"hide nil", func(ctx context.Context, o *Object) error { return o.Hide(ctx, nil) }},
		{"hide zero", func(ctx context.Context, o *Object) error { return o.Hide(ctx, &SSECKey{}) }},
		{"hide multiple", func(ctx context.Context, o *Object) error { return o.Hide(ctx, key, key) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			before := len(srv.Requests())
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tc.run(ctx, bucket.Object("protected")); err == nil {
				t.Fatal("invalid key accepted")
			}
			if got := len(srv.Requests()) - before; got != 0 {
				t.Fatalf("requests = %d, want 0", got)
			}
		})
	}
}
