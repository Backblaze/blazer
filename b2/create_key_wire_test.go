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

	"github.com/Backblaze/blazer/base"
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

func createKeyWireBucket(t *testing.T, srv *b2wiretest.Server, client *Client) *Bucket {
	t.Helper()
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bucket-from-list","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1}]}`)
	bucket, err := client.Bucket(context.Background(), "bucket")
	if err != nil {
		t.Fatalf("Bucket: %v", err)
	}
	return bucket
}

func bucketCreateKeyRequest(t *testing.T) b2wiretest.Request {
	t.Helper()
	srv, client := newCreateKeyWire(t)
	bucket := createKeyWireBucket(t, srv, client)
	if _, err := bucket.CreateKey(context.Background(), "k2", Capabilities("readFiles"), Lifetime(time.Hour), Prefix("p/")); err != nil {
		t.Fatalf("Bucket.CreateKey: %v", err)
	}
	return srv.Last(t, "b2_create_key")
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
func TestBucketCreateKeySendsTheBucketRestriction(t *testing.T) {
	request := bucketCreateKeyRequest(t)
	if request.Path != "/b2api/v3/b2_create_key" {
		t.Errorf("b2_create_key path = %q, want v3", request.Path)
	}
	b2wiretest.AssertJSON(t, request, "bucketId", "bucket-from-list")
	b2wiretest.AssertJSON(t, request, "namePrefix", "p/")
	b2wiretest.AssertNoJSON(t, request, "bucketIds")
}

// B2 b2_create_key request fields: https://www.backblaze.com/apidocs/b2-create-key
func TestBucketCreateKeyForwardsFields(t *testing.T) {
	request := bucketCreateKeyRequest(t)
	b2wiretest.AssertJSON(t, request, "keyName", "k2")
	b2wiretest.AssertJSON(t, request, "capabilities", []string{"readFiles"})
	b2wiretest.AssertJSON(t, request, "validDurationInSeconds", 3600)
}

// B2 b2_create_key error responses: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyReturnsB2Errors(t *testing.T) {
	ctx := context.Background()
	statuses := []struct {
		status int
		code   string
	}{
		{status: http.StatusBadRequest, code: "bad_request"},
		{status: http.StatusForbidden, code: "access_denied"},
	}
	shapes := []struct {
		name   string
		bucket bool
		opts   []KeyOption
	}{
		{name: "account", opts: []KeyOption{Capabilities("readFiles")}},
		{name: "one bucket", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1")}},
		{name: "two buckets", opts: []KeyOption{Capabilities("readFiles"), BucketIDs("b-1", "b-2")}},
		{name: "bucket key", bucket: true},
	}

	for _, status := range statuses {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("%d/%s", status.status, shape.name), func(t *testing.T) {
				srv, client := newCreateKeyWire(t)
				srv.Reply("b2_create_key", status.status, fmt.Sprintf(`{"status":%d,"code":%q,"message":"rejected"}`, status.status, status.code))

				var (
					key *Key
					err error
				)
				if shape.bucket {
					bucket := createKeyWireBucket(t, srv, client)
					key, err = bucket.CreateKey(ctx, "k1", Capabilities("readFiles"))
				} else {
					key, err = client.CreateKey(ctx, "k1", shape.opts...)
				}
				if err == nil {
					t.Error("CreateKey error = nil, want B2 error")
				}
				if gotStatus, _ := base.Code(err); gotStatus != status.status {
					t.Errorf("base.Code(err) status = %d, want %d", gotStatus, status.status)
				}
				if _, gotCode, _ := base.MsgCode(err); gotCode != status.code {
					t.Errorf("base.MsgCode(err) code = %q, want %q", gotCode, status.code)
				}
				if key != nil {
					t.Errorf("CreateKey key = %#v, want nil", key)
				}
				if calls := srv.Calls("b2_create_key"); len(calls) != 1 {
					t.Errorf("b2_create_key calls = %d, want 1", len(calls))
				}
			})
		}
	}
}

// B2 b2_create_key response fields: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyReturnsTheResponseFields(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		path string
		call func(*testing.T, *b2wiretest.Server, *Client) (*Key, error)
	}{
		{
			name: "client",
			path: "/b2api/v3/b2_create_key",
			call: func(_ *testing.T, _ *b2wiretest.Server, client *Client) (*Key, error) {
				return client.CreateKey(ctx, "k1", Capabilities("readFiles"))
			},
		},
		{
			name: "bucket",
			path: "/b2api/v3/b2_create_key",
			call: func(t *testing.T, srv *b2wiretest.Server, client *Client) (*Key, error) {
				return createKeyWireBucket(t, srv, client).CreateKey(ctx, "k1", Capabilities("readFiles"))
			},
		},
		{
			name: "multi-bucket",
			path: "/b2api/v4/b2_create_key",
			call: func(_ *testing.T, _ *b2wiretest.Server, client *Client) (*Key, error) {
				return client.CreateKey(ctx, "k1", Capabilities("readFiles"), BucketIDs("b-1", "b-2"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, client := newCreateKeyWire(t)
			srv.Reply("b2_create_key", http.StatusOK, `{"applicationKeyId":"key-id","applicationKey":"secret","keyName":"k1","capabilities":["readFiles"],"expirationTimestamp":1767225600000}`)

			key, err := test.call(t, srv, client)
			if err != nil {
				t.Fatalf("CreateKey: %v", err)
			}
			if key == nil {
				t.Fatal("CreateKey key = nil, want key")
			}
			calls := srv.Calls("b2_create_key")
			if len(calls) != 1 {
				t.Fatalf("b2_create_key calls = %d, want 1", len(calls))
			}
			if calls[0].Path != test.path {
				t.Errorf("b2_create_key path = %q, want %q", calls[0].Path, test.path)
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
		})
	}
}

// B2 b2_create_key bucket restriction rules: https://www.backblaze.com/apidocs/b2-create-key
func TestCreateKeyRejectsBadOptionsWithoutARequest(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		bucket  bool
		wantErr string
	}{
		{
			name:    "client prefix without bucket",
			wantErr: "Prefix requires at least one bucket; use BucketIDs or create the key via (*Bucket).CreateKey",
		},
		{
			name:    "bucket key with BucketIDs",
			bucket:  true,
			wantErr: "BucketIDs cannot be combined with (*Bucket).CreateKey; use (*Client).CreateKey to request a multi-bucket key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, client := newCreateKeyWire(t)
			var (
				key *Key
				err error
			)
			if test.bucket {
				key, err = createKeyWireBucket(t, srv, client).CreateKey(ctx, "k1", BucketIDs("b-1"))
			} else {
				key, err = client.CreateKey(ctx, "k1", Prefix("p/"))
			}
			if err == nil {
				t.Errorf("CreateKey error = nil, want %q", test.wantErr)
			} else if err.Error() != test.wantErr {
				t.Errorf("CreateKey error = %q, want %q", err, test.wantErr)
			}
			if key != nil {
				t.Errorf("CreateKey key = %#v, want nil", key)
			}
			if calls := srv.Calls("b2_create_key"); len(calls) != 0 {
				t.Errorf("b2_create_key calls = %d, want 0", len(calls))
			}
		})
	}
}
