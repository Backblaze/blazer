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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type keyRestrictionsWire struct {
	createV4 map[string]any
	mu       sync.Mutex
	server   *httptest.Server
}

func newKeyRestrictionsWire(t *testing.T) (*Client, *keyRestrictionsWire) {
	t.Helper()
	wire := &keyRestrictionsWire{}
	wire.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, wire.server.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_, _ = fmt.Fprint(w, `{"buckets":[]}`)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			_, _ = fmt.Fprint(w, updateBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_create_key") && strings.Contains(r.URL.Path, "/v4/"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read create-key request: %v", err)
				return
			}
			var sent map[string]any
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode create-key request: %v", err)
			}
			wire.mu.Lock()
			wire.createV4 = sent
			wire.mu.Unlock()
			_, _ = fmt.Fprint(w, `{"accountId":"a","applicationKeyId":"key-v4","applicationKey":"secret-v4","capabilities":["listFiles"],"keyName":"v4","expirationTimestamp":0,"bucketIds":["b1","b2"],"namePrefix":"p/"}`)
		case strings.HasSuffix(r.URL.Path, "/b2_create_key"):
			_, _ = fmt.Fprint(w, `{"accountId":"a","applicationKeyId":"key-v3","applicationKey":"secret-v3","capabilities":["listFiles"],"keyName":"v3","expirationTimestamp":0,"bucketId":"bid","namePrefix":"bucket/"}`)
		case strings.HasSuffix(r.URL.Path, "/b2_list_keys"):
			_, _ = fmt.Fprint(w, `{"keys":[{"accountId":"a","applicationKeyId":"key-v4","capabilities":["listFiles"],"keyName":"v4","expirationTimestamp":0,"bucketIds":["b1","b2"],"namePrefix":"p/"},{"accountId":"a","applicationKeyId":"key-unrestricted","capabilities":["listBuckets"],"keyName":"unrestricted","expirationTimestamp":0,"bucketIds":null,"namePrefix":null},{"accountId":"a","applicationKeyId":"key-empty","capabilities":["listBuckets"],"keyName":"empty","expirationTimestamp":0,"bucketIds":[],"namePrefix":null}],"nextApplicationKeyId":null}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(wire.server.Close)

	client, err := NewClient(context.Background(), "a", "k", APIBase(wire.server.URL))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, wire
}

func assertKeyRestrictions(t *testing.T, key *Key, wantIDs []string, wantPrefix string) {
	t.Helper()
	if got := key.BucketIDs(); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("BucketIDs() = %#v, want %#v", got, wantIDs)
	}
	if got := key.Prefix(); got != wantPrefix {
		t.Errorf("Prefix() = %q, want %q", got, wantPrefix)
	}
}

func TestKeyRestrictionsWire(t *testing.T) {
	ctx := context.Background()
	client, wire := newKeyRestrictionsWire(t)

	t.Run("client create key", func(t *testing.T) {
		key, err := client.CreateKey(ctx, "v4", Capabilities("listFiles"), BucketIDs("b1", "b2"), Prefix("p/"))
		if err != nil {
			t.Fatalf("CreateKey: %v", err)
		}
		assertKeyRestrictions(t, key, []string{"b1", "b2"}, "p/")

		wire.mu.Lock()
		sent := wire.createV4
		wire.mu.Unlock()
		if got := sent["bucketIds"]; !reflect.DeepEqual(got, []any{"b1", "b2"}) {
			t.Errorf("create request bucketIds = %#v, want %#v", got, []any{"b1", "b2"})
		}
		if got := sent["namePrefix"]; got != "p/" {
			t.Errorf("create request namePrefix = %#v, want %q", got, "p/")
		}

		ids := key.BucketIDs()
		ids[0] = "changed"
		assertKeyRestrictions(t, key, []string{"b1", "b2"}, "p/")
	})

	t.Run("bucket create key", func(t *testing.T) {
		bucket, err := client.NewBucket(ctx, "n", nil)
		if err != nil {
			t.Fatalf("NewBucket: %v", err)
		}
		key, err := bucket.CreateKey(ctx, "v3", Capabilities("listFiles"), Prefix("bucket/"))
		if err != nil {
			t.Fatalf("Bucket.CreateKey: %v", err)
		}
		assertKeyRestrictions(t, key, []string{"bid"}, "bucket/")
	})

	t.Run("list keys", func(t *testing.T) {
		keys, next, err := client.ListKeys(ctx, 10, "")
		if err != nil && err != io.EOF {
			t.Fatalf("ListKeys: %v", err)
		}
		if next != "" {
			t.Errorf("next = %q, want empty", next)
		}
		if len(keys) != 3 {
			t.Fatalf("ListKeys returned %d keys, want 3", len(keys))
		}
		assertKeyRestrictions(t, keys[0], []string{"b1", "b2"}, "p/")
		assertKeyRestrictions(t, keys[1], nil, "")
		assertKeyRestrictions(t, keys[2], nil, "")
	})
}
