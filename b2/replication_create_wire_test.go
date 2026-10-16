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

const replicationCreateResponse = `{
	"bucketId":"bucket-id","bucketName":"bucket","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1,
	"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},
	"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":{"sourceApplicationKeyId":"source-key","replicationRules":[{"replicationRuleName":"disabled-rule","destinationBucketId":"destination-bucket-id","fileNamePrefix":"prefix/","includeExistingFiles":true,"isEnabled":false,"priority":1}]},"asReplicationDestination":{"sourceToDestinationKeyMapping":{"source-key":"destination-key"}}}}
}`

type replicationCreateWire struct {
	t *testing.T

	srv *httptest.Server
	mu  sync.Mutex

	creates [][]byte
	updates [][]byte
}

func newReplicationCreateWire(t *testing.T) *replicationCreateWire {
	t.Helper()
	w := &replicationCreateWire{t: t}
	w.srv = httptest.NewServer(http.HandlerFunc(w.serveHTTP))
	t.Cleanup(w.srv.Close)
	return w
}

func (w *replicationCreateWire) serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
		_, _ = fmt.Fprintf(rw, updateAuthJSON, w.srv.URL)
	case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
		w.mu.Lock()
		created := len(w.creates) > 0
		w.mu.Unlock()
		if created {
			_, _ = fmt.Fprintf(rw, `{"buckets":[%s]}`, replicationCreateResponse)
		} else {
			_, _ = fmt.Fprint(rw, `{"buckets":[]}`)
		}
	case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
		w.record(takeBody(w.t, r), true)
		_, _ = fmt.Fprint(rw, replicationCreateResponse)
	case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
		w.record(takeBody(w.t, r), false)
		_, _ = fmt.Fprint(rw, replicationCreateResponse)
	default:
		w.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(rw, r)
	}
}

func takeBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "t" {
		t.Errorf("request = %s authorization=%q, want POST with account token", r.Method, r.Header.Get("Authorization"))
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}
	return body
}

func (w *replicationCreateWire) record(body []byte, create bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if create {
		w.creates = append(w.creates, body)
		return
	}
	w.updates = append(w.updates, body)
}

func replicationField(t *testing.T, body []byte) (json.RawMessage, bool) {
	t.Helper()
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	field, ok := request["replicationConfiguration"]
	return field, ok
}

func replicationCreateConfig() *ReplicationConfiguration {
	return &ReplicationConfiguration{
		AsReplicationSource: AsReplicationSource{
			SourceApplicationKeyID: "source-key",
			ReplicationRules: []ReplicationRules{{
				ReplicationRuleName:  "disabled-rule",
				DestinationBucketID:  "destination-bucket-id",
				FileNamePrefix:       "prefix/",
				IncludeExistingFiles: true,
				IsEnabled:            false,
				Priority:             1,
			}},
		},
		AsReplicationDestination: &AsReplicationDestination{
			SourceToDestinationKeyMapping: map[string]string{"source-key": "destination-key"},
		},
	}
}

func TestNewBucketReplicationConfigurationWireRoundTrip(t *testing.T) {
	wire := newReplicationCreateWire(t)
	client, err := NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	want := replicationCreateConfig()
	bucket, err := client.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private, ReplicationConfig: want})
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.creates) != 1 {
		t.Fatalf("create requests = %d, want 1", len(wire.creates))
	}
	created, ok := replicationField(t, wire.creates[0])
	if !ok || len(created) == 0 || string(created) == "null" {
		t.Fatalf("create replicationConfiguration = %s, want the configured source and destination", created)
	}
	attrs, err := bucket.Attrs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attrs.ReplicationConfig, want) {
		t.Errorf("Attrs().ReplicationConfig = %#v, want %#v", attrs.ReplicationConfig, want)
	}
	if attrs.ReplicationConfig.AsReplicationSource.ReplicationRules[0].IsEnabled {
		t.Error("disabled replication rule read back as enabled")
	}

	if err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: want}); err != nil {
		t.Fatal(err)
	}
	if len(wire.updates) != 1 {
		t.Fatalf("update requests = %d, want 1", len(wire.updates))
	}
	updated, ok := replicationField(t, wire.updates[0])
	if !ok {
		t.Fatal("update omitted replicationConfiguration")
	}
	if string(updated) != string(created) {
		t.Errorf("update replicationConfiguration = %s, want byte-identical create shape %s", updated, created)
	}
}

func TestNewBucketOmitsUnsetReplicationConfiguration(t *testing.T) {
	wire := newReplicationCreateWire(t)
	client, err := NewClient(context.Background(), "account", "key", APIBase(wire.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(context.Background(), "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	if bucket == nil || len(wire.creates) != 1 {
		t.Fatalf("NewBucket = %v with %d create request(s), want bucket and one request", bucket, len(wire.creates))
	}
	if field, ok := replicationField(t, wire.creates[0]); ok {
		t.Errorf("unset create sent replicationConfiguration = %s", field)
	}
}
