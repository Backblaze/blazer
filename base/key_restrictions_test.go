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

package base

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func newKeyRestrictionsB2(t *testing.T, createResponse, listResponse string) *B2 {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_create_key"):
			_, _ = fmt.Fprint(w, createResponse)
		case strings.HasSuffix(r.URL.Path, "/b2_list_keys"):
			_, _ = fmt.Fprint(w, listResponse)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	return b
}

func assertBaseKeyRestrictions(t *testing.T, key *Key, wantIDs []string, wantPrefix string) {
	t.Helper()
	if !reflect.DeepEqual(key.BucketIDs, wantIDs) {
		t.Errorf("BucketIDs = %#v, want %#v", key.BucketIDs, wantIDs)
	}
	if key.Prefix != wantPrefix {
		t.Errorf("Prefix = %q, want %q", key.Prefix, wantPrefix)
	}
}

func TestCreateKeyNormalizesRestrictions(t *testing.T) {
	const listResponse = `{"keys":[],"nextApplicationKeyId":null}`
	tests := []struct {
		name           string
		createResponse string
		create         func(context.Context, *B2) (*Key, error)
		wantIDs        []string
		wantPrefix     string
	}{
		{
			name:           "v3 bucketId",
			createResponse: `{"accountId":"account-id","applicationKeyId":"key-v3","applicationKey":"secret-v3","capabilities":["listFiles"],"keyName":"v3","expirationTimestamp":0,"bucketId":"bucket-v3","namePrefix":"v3/"}`,
			create: func(ctx context.Context, b *B2) (*Key, error) {
				return b.CreateKey(ctx, "v3", []string{"listFiles"}, 0, "bucket-v3", "v3/")
			},
			wantIDs:    []string{"bucket-v3"},
			wantPrefix: "v3/",
		},
		{
			name:           "v4 bucketIds",
			createResponse: `{"accountId":"account-id","applicationKeyId":"key-v4","applicationKey":"secret-v4","capabilities":["listFiles"],"keyName":"v4","expirationTimestamp":0,"bucketIds":["bucket-a","bucket-b"],"namePrefix":"v4/"}`,
			create: func(ctx context.Context, b *B2) (*Key, error) {
				return b.CreateKeyMultiBucket(ctx, "v4", []string{"listFiles"}, 0, []string{"bucket-a", "bucket-b"}, "v4/")
			},
			wantIDs:    []string{"bucket-a", "bucket-b"},
			wantPrefix: "v4/",
		},
		{
			name:           "unrestricted",
			createResponse: `{"accountId":"account-id","applicationKeyId":"key-unrestricted","applicationKey":"secret-unrestricted","capabilities":["listBuckets"],"keyName":"unrestricted","expirationTimestamp":0,"bucketIds":null,"namePrefix":null}`,
			create: func(ctx context.Context, b *B2) (*Key, error) {
				return b.CreateKeyMultiBucket(ctx, "unrestricted", []string{"listBuckets"}, 0, nil, "")
			},
		},
		{
			name:           "empty bucketIds",
			createResponse: `{"accountId":"account-id","applicationKeyId":"key-empty","applicationKey":"secret-empty","capabilities":["listBuckets"],"keyName":"empty","expirationTimestamp":0,"bucketIds":[],"namePrefix":null}`,
			create: func(ctx context.Context, b *B2) (*Key, error) {
				return b.CreateKeyMultiBucket(ctx, "empty", []string{"listBuckets"}, 0, nil, "")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := newKeyRestrictionsB2(t, test.createResponse, listResponse)
			key, err := test.create(context.Background(), b)
			if err != nil {
				t.Fatalf("CreateKey: %v", err)
			}
			assertBaseKeyRestrictions(t, key, test.wantIDs, test.wantPrefix)
		})
	}
}

func TestListKeysNormalizesRestrictions(t *testing.T) {
	const listResponse = `{"keys":[` +
		`{"accountId":"account-id","applicationKeyId":"key-v4","capabilities":["listFiles"],"keyName":"v4","expirationTimestamp":0,"bucketIds":["bucket-a","bucket-b"],"namePrefix":"v4/"},` +
		`{"accountId":"account-id","applicationKeyId":"key-unrestricted","capabilities":["listBuckets"],"keyName":"unrestricted","expirationTimestamp":0,"bucketIds":null,"namePrefix":null},` +
		`{"accountId":"account-id","applicationKeyId":"key-empty","capabilities":["listBuckets"],"keyName":"empty","expirationTimestamp":0,"bucketIds":[],"namePrefix":null}` +
		`],"nextApplicationKeyId":null}`
	b := newKeyRestrictionsB2(t, `{}`, listResponse)

	keys, next, err := b.ListKeys(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if next != "" {
		t.Errorf("next = %q, want empty", next)
	}
	if len(keys) != 3 {
		t.Fatalf("ListKeys returned %d keys, want 3", len(keys))
	}
	assertBaseKeyRestrictions(t, keys[0], []string{"bucket-a", "bucket-b"}, "v4/")
	assertBaseKeyRestrictions(t, keys[1], nil, "")
	assertBaseKeyRestrictions(t, keys[2], nil, "")
}
