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
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func newCreateKeyWire(t *testing.T) (*b2wiretest.Server, *Client) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(`{"accountId":"acct-1","authorizationToken":"test-token","applicationKeyExpirationTimestamp":0,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"apiUrl":%[1]q,"capabilities":[],"downloadUrl":%[1]q,"storageApi":"storage","recommendedPartSize":100000000,"s3ApiUrl":%[1]q,"allowed":{"buckets":null,"capabilities":[],"namePrefix":null}}}}`, srv.URL()))
	srv.Reply("b2_create_key", http.StatusOK, `{"applicationKeyId":"default-key-id","applicationKey":"default-secret","keyName":"default-key","capabilities":["readFiles"],"expirationTimestamp":0}`)

	client, err := NewClient(context.Background(), "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	return srv, client
}

// B2 b2_create_key request fields: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyRequestBody(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		opts   []KeyOption
		path   string
		want   map[string]any
		absent []string
	}{
		{
			name: "account-wide with lifetime",
			opts: []KeyOption{
				Capabilities("listBuckets", "readFiles"),
				Lifetime(time.Hour),
			},
			path: "/b2api/v3/b2_create_key",
			want: map[string]any{
				"accountId":              "acct-1",
				"keyName":                "k1",
				"capabilities":           []string{"listBuckets", "readFiles"},
				"validDurationInSeconds": 3600,
			},
			absent: []string{"bucketId", "bucketIds", "namePrefix"},
		},
		{
			name: "account-wide without lifetime",
			opts: []KeyOption{
				Capabilities("listBuckets", "readFiles"),
			},
			path: "/b2api/v3/b2_create_key",
			want: map[string]any{
				"accountId":    "acct-1",
				"keyName":      "k1",
				"capabilities": []string{"listBuckets", "readFiles"},
			},
			absent: []string{"validDurationInSeconds", "bucketId", "bucketIds", "namePrefix"},
		},
		{
			name: "one bucket with prefix",
			opts: []KeyOption{
				Capabilities("listBuckets", "readFiles"),
				BucketIDs("b-1"),
				Prefix("logs/"),
			},
			path: "/b2api/v3/b2_create_key",
			want: map[string]any{
				"accountId":    "acct-1",
				"keyName":      "k1",
				"capabilities": []string{"listBuckets", "readFiles"},
				"bucketId":     "b-1",
				"namePrefix":   "logs/",
			},
			absent: []string{"validDurationInSeconds", "bucketIds"},
		},
		{
			name: "several buckets",
			opts: []KeyOption{
				Capabilities("listBuckets", "readFiles"),
				BucketIDs("b-1", "b-2"),
			},
			path: "/b2api/v4/b2_create_key",
			want: map[string]any{
				"accountId":    "acct-1",
				"keyName":      "k1",
				"capabilities": []string{"listBuckets", "readFiles"},
				"bucketIds":    []string{"b-1", "b-2"},
			},
			absent: []string{"validDurationInSeconds", "bucketId", "namePrefix"},
		},
		{
			name: "several buckets with lifetime and prefix",
			opts: []KeyOption{
				Capabilities("listBuckets", "readFiles"),
				BucketIDs("b-1", "b-2"),
				Lifetime(time.Hour),
				Prefix("logs/"),
			},
			path: "/b2api/v4/b2_create_key",
			want: map[string]any{
				"accountId":              "acct-1",
				"keyName":                "k1",
				"capabilities":           []string{"listBuckets", "readFiles"},
				"bucketIds":              []string{"b-1", "b-2"},
				"validDurationInSeconds": 3600,
				"namePrefix":             "logs/",
			},
			absent: []string{"bucketId"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, client := newCreateKeyWire(t)
			if _, err := client.CreateKey(ctx, "k1", test.opts...); err != nil {
				t.Fatalf("CreateKey: %v", err)
			}
			calls := srv.Calls("b2_create_key")
			if len(calls) != 1 {
				t.Fatalf("b2_create_key calls = %d, want 1", len(calls))
			}
			request := calls[0]
			if request.Method != http.MethodPost || request.Path != test.path {
				t.Errorf("request = %s %s, want POST %s", request.Method, request.Path, test.path)
			}
			for field, want := range test.want {
				b2wiretest.AssertJSON(t, request, field, want)
			}
			for _, field := range test.absent {
				b2wiretest.AssertNoJSON(t, request, field)
			}
		})
	}
}

// B2 b2_create_key bucket restrictions: https://www.backblaze.com/apidocs/b2-create-key
func TestBucketCreateKeySendsTheBucketID(t *testing.T) {
	ctx := context.Background()
	srv, client := newCreateKeyWire(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bucket-from-list","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1}]}`)

	bucket, err := client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	if _, err := bucket.CreateKey(ctx, "k2", Capabilities("readFiles"), Lifetime(time.Hour), Prefix("p/")); err != nil {
		t.Fatalf("Bucket.CreateKey: %v", err)
	}

	request := srv.Last(t, "b2_create_key")
	if request.Path != "/b2api/v3/b2_create_key" {
		t.Errorf("b2_create_key path = %q, want v3", request.Path)
	}
	b2wiretest.AssertJSON(t, request, "keyName", "k2")
	b2wiretest.AssertJSON(t, request, "capabilities", []string{"readFiles"})
	b2wiretest.AssertJSON(t, request, "validDurationInSeconds", 3600)
	b2wiretest.AssertJSON(t, request, "bucketId", "bucket-from-list")
	b2wiretest.AssertJSON(t, request, "namePrefix", "p/")
	b2wiretest.AssertNoJSON(t, request, "bucketIds")
}

// B2 b2_create_key error responses: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyReturnsB2Errors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		status int
		code   string
		bucket bool
		opts   []KeyOption
	}{
		{name: "400/account", status: http.StatusBadRequest, code: "bad_request", opts: []KeyOption{Capabilities("readFiles")}},
		{name: "400/one bucket", status: http.StatusBadRequest, code: "bad_request", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1")}},
		{name: "400/two buckets", status: http.StatusBadRequest, code: "bad_request", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1", "b-2")}},
		{name: "400/bucket key", status: http.StatusBadRequest, code: "bad_request", bucket: true},
		{name: "403/account", status: http.StatusForbidden, code: "access_denied", opts: []KeyOption{Capabilities("readFiles")}},
		{name: "403/one bucket", status: http.StatusForbidden, code: "access_denied", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1")}},
		{name: "403/two buckets", status: http.StatusForbidden, code: "access_denied", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1", "b-2")}},
		{name: "403/bucket key", status: http.StatusForbidden, code: "access_denied", bucket: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, client := newCreateKeyWire(t)
			srv.Reply("b2_create_key", test.status, fmt.Sprintf(`{"status":%d,"code":%q,"message":"rejected"}`, test.status, test.code))

			var (
				key *Key
				err error
			)
			if test.bucket {
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bucket-from-list","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1}]}`)
				bucket, bucketErr := client.Bucket(ctx, "bucket")
				if bucketErr != nil {
					t.Fatalf("Bucket: %v", bucketErr)
				}
				key, err = bucket.CreateKey(ctx, "k1", Capabilities("readFiles"))
			} else {
				key, err = client.CreateKey(ctx, "k1", test.opts...)
			}
			if err == nil {
				t.Error("CreateKey error = nil, want B2 error")
			}
			if key != nil {
				t.Errorf("CreateKey key = %#v, want nil", key)
			}
		})
	}
}

// B2 b2_create_key response fields: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyReturnsTheResponseFields(t *testing.T) {
	ctx := context.Background()
	srv, client := newCreateKeyWire(t)
	srv.Reply("b2_create_key", http.StatusOK, `{"applicationKeyId":"key-id","applicationKey":"secret","keyName":"k1","capabilities":["readFiles"],"expirationTimestamp":1767225600000}`)

	key, err := client.CreateKey(ctx, "k1", Capabilities("readFiles"))
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if key == nil {
		t.Fatal("CreateKey key = nil, want key")
	}
	if key.ID() != "key-id" {
		t.Errorf("ID = %q, want key-id", key.ID())
	}
	if key.Secret() != "secret" {
		t.Errorf("Secret = %q, want secret", key.Secret())
	}
	if key.Name() != "k1" {
		t.Errorf("Name = %q, want k1", key.Name())
	}
	if want := []string{"readFiles"}; !reflect.DeepEqual(key.Capabilities(), want) {
		t.Errorf("Capabilities = %q, want %q", key.Capabilities(), want)
	}
	if want := time.UnixMilli(1767225600000); !key.Expires().Equal(want) {
		t.Errorf("Expires = %v, want %v", key.Expires(), want)
	}
}

// B2 b2_create_key bucket restriction rules: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyRejectsBadOptionsWithoutARequest(t *testing.T) {
	ctx := context.Background()
	srv, client := newCreateKeyWire(t)

	if key, err := client.CreateKey(ctx, "k1", Prefix("p/")); err == nil || key != nil {
		t.Errorf("Client.CreateKey(Prefix) = (%#v, %v), want nil key and error", key, err)
	}

	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bucket-from-list","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1}]}`)
	bucket, err := client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	if key, err := bucket.CreateKey(ctx, "k1", BucketIDs("b-1")); err == nil || key != nil {
		t.Errorf("Bucket.CreateKey(BucketIDs) = (%#v, %v), want nil key and error", key, err)
	}
	if calls := srv.Calls("b2_create_key"); len(calls) != 0 {
		t.Errorf("b2_create_key calls = %d, want 0", len(calls))
	}
}
