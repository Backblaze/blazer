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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type setupReplicationWire struct {
	updates []map[string]interface{}
	paths   []string
}

func newSetupReplicationWire(t *testing.T, ctx context.Context) (*setupReplicationWire, *Bucket, *Bucket) {
	t.Helper()
	wire := &setupReplicationWire{}
	buckets := map[string]map[string]interface{}{}
	for _, entry := range []struct{ name, id string }{{"source", "src-id"}, {"destination", "dst-id"}} {
		bucket := bucketWithEmptyDefaultSSE(map[string]interface{}{"bucketName": entry.name, "bucketType": "allPrivate"})
		bucket["bucketId"] = entry.id
		buckets[entry.name] = bucket
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			var request struct {
				BucketName string `json:"bucketName"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode list: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"buckets": []interface{}{buckets[request.BucketName]}})
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update: %v", err)
			}
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "t" {
				t.Errorf("update wire = %s auth=%q", r.Method, r.Header.Get("Authorization"))
			}
			wire.updates = append(wire.updates, body)
			wire.paths = append(wire.paths, r.URL.Path)
			name := "source"
			if body["bucketId"] == "dst-id" {
				name = "destination"
			}
			buckets[name]["replicationConfiguration"] = map[string]interface{}{"isClientAuthorizedToRead": true, "value": body["replicationConfiguration"]}
			buckets[name]["revision"] = float64(2)
			_ = json.NewEncoder(w).Encode(buckets[name])
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	src, err := client.Bucket(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := client.Bucket(ctx, "destination")
	if err != nil {
		t.Fatal(err)
	}
	return wire, src, dst
}

func TestSetupReplicationPublicWire(t *testing.T) {
	ctx := context.Background()
	wire, src, dst := newSetupReplicationWire(t, ctx)
	// https://www.backblaze.com/apidocs/b2-update-bucket documents priority 1.
	if err := SetupReplication(ctx, src, dst, "source-key", "destination-key", WithRuleName("rule"), WithFileNamePrefix("logs/"), WithRulePriority(1)); err != nil {
		t.Fatal(err)
	}
	if len(wire.updates) != 2 || wire.updates[0]["bucketId"] != "dst-id" || wire.updates[1]["bucketId"] != "src-id" {
		t.Fatalf("wire update order = %#v, want destination then source", wire.updates)
	}
	for _, path := range wire.paths {
		if !strings.HasSuffix(path, "/b2_update_bucket") {
			t.Fatalf("update path = %q", path)
		}
	}
	dstConfig := wire.updates[0]["replicationConfiguration"].(map[string]interface{})
	dstRole := dstConfig["asReplicationDestination"].(map[string]interface{})
	mapping := dstRole["sourceToDestinationKeyMapping"].(map[string]interface{})
	if mapping["source-key"] != "destination-key" {
		t.Fatalf("destination wire mapping = %#v", mapping)
	}
	srcConfig := wire.updates[1]["replicationConfiguration"].(map[string]interface{})
	srcRole := srcConfig["asReplicationSource"].(map[string]interface{})
	rules := srcRole["replicationRules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("source wire rules = %#v, want one", rules)
	}
	rule := rules[0].(map[string]interface{})
	if srcRole["sourceApplicationKeyId"] != "source-key" || rule["destinationBucketId"] != "dst-id" || rule["fileNamePrefix"] != "logs/" || rule["replicationRuleName"] != "rule" || rule["priority"] != float64(1) {
		t.Fatalf("source wire role = %#v", srcRole)
	}
	if err := SetupReplication(ctx, src, dst, "source-key", "destination-key", WithRuleName("rule"), WithFileNamePrefix("logs/")); err != nil {
		t.Fatal(err)
	}
	if len(wire.updates) != 2 {
		t.Fatalf("idempotent rerun sent %d updates, want 2", len(wire.updates))
	}
}

func TestSetupReplicationDefaultPriorityOnWire(t *testing.T) {
	ctx := context.Background()
	wire, src, dst := newSetupReplicationWire(t, ctx)
	// 128 is the SDK's default for a bucket's first rule.
	if err := SetupReplication(ctx, src, dst, "source-key", "destination-key", WithRuleName("default-rule"), WithFileNamePrefix("logs/")); err != nil {
		t.Fatal(err)
	}
	if len(wire.updates) != 2 || wire.updates[1]["bucketId"] != "src-id" {
		t.Fatalf("wire updates = %#v, want destination then source", wire.updates)
	}
	sourceConfig := wire.updates[1]["replicationConfiguration"].(map[string]interface{})
	sourceRole := sourceConfig["asReplicationSource"].(map[string]interface{})
	rules := sourceRole["replicationRules"].([]interface{})
	if len(rules) != 1 {
		t.Fatalf("source wire rules = %#v, want exactly one", rules)
	}
	rule := rules[0].(map[string]interface{})
	if rule["priority"] != float64(128) || rule["replicationRuleName"] != "default-rule" || rule["destinationBucketId"] != "dst-id" || rule["fileNamePrefix"] != "logs/" {
		t.Fatalf("source wire rule = %#v, want default priority and requested fields", rule)
	}
}

func TestFailedReplicationUpdateDoesNotPoisonCachedConfig(t *testing.T) {
	ctx := context.Background()
	bucket := bucketWithEmptyDefaultSSE(map[string]interface{}{"bucketName": "source", "bucketType": "allPrivate"})
	bucket["bucketId"] = "src-id"
	var updateBodies []map[string]interface{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"buckets": []interface{}{bucket}})
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode update: %v", err)
			}
			updateBodies = append(updateBodies, body)
			if len(updateBodies) == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"bad_request","message":"rejected","status":400}`))
				return
			}
			_ = json.NewEncoder(w).Encode(bucket)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	src, err := client.Bucket(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	config := &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "source-key"}}
	if err := src.Update(ctx, &BucketAttrs{ReplicationConfig: config}); err == nil {
		t.Fatal("first update succeeded, want server rejection")
	}
	if err := src.Update(ctx, &BucketAttrs{Info: map[string]string{"new": "value"}}); err != nil {
		t.Fatal(err)
	}
	if len(updateBodies) != 2 {
		t.Fatalf("update requests = %d, want 2", len(updateBodies))
	}
	if _, present := updateBodies[1]["replicationConfiguration"]; present {
		t.Fatalf("unrelated retry resent rejected replication config: %#v", updateBodies[1])
	}
}
