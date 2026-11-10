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

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const destinationReplicationUpdateJSON = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":{"sourceToDestinationKeyMapping":{"source-key":"old-destination-key"}}}}}`

const unreadableReplicationUpdateJSON = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":false,"value":null}}`

func replicationUpdateWireBucket(t *testing.T, response string) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+response+`]}`)
	// Successful, independent replies ensure a missing validation guard cannot
	// pass because the server returned an unrelated error.
	srv.Reply("b2_update_bucket", http.StatusOK, response)
	bucket, err := liveOnlyClient(t, srv).Bucket(context.Background(), "n")
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

// https://www.backblaze.com/apidocs/b2-update-bucket documents partial updates.
func TestBucketUpdateOmitsUnchangedReplicationWire(t *testing.T) {
	for _, tc := range []struct {
		name, response string
	}{
		{"source", sourceReplBucketJSON},
		{"destination", destinationReplicationUpdateJSON},
		{"both roles", bothReplBucketJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := replicationUpdateWireBucket(t, tc.response)
			srv.StrictReplySequence("b2_update_bucket",
				b2wiretest.Response{Body: tc.response},
				b2wiretest.Response{Body: tc.response},
			)
			for _, info := range []map[string]string{{"first": "one"}, {"second": "two"}} {
				if err := bucket.Update(context.Background(), &BucketAttrs{Info: info}); err != nil {
					t.Fatal(err)
				}
			}
			updates := srv.Calls("b2_update_bucket")
			if len(updates) != 2 {
				t.Fatalf("update requests = %d, want 2", len(updates))
			}
			for i, request := range updates {
				if request.Method != http.MethodPost || request.Path != "/b2api/v4/b2_update_bucket" {
					t.Errorf("update %d = %s %s, want POST /b2api/v4/b2_update_bucket", i+1, request.Method, request.Path)
				}
				b2wiretest.AssertJSON(t, request, "bucketId", "bid")
				b2wiretest.AssertNoJSON(t, request, "replicationConfiguration")
			}
			b2wiretest.AssertJSON(t, updates[0], "bucketInfo", map[string]string{"first": "one"})
			b2wiretest.AssertJSON(t, updates[1], "bucketInfo", map[string]string{"second": "two"})
		})
	}
}

// https://www.backblaze.com/apidocs/b2-update-bucket requires a replication role;
// an unreadable existing configuration cannot be merged safely (#98).
func TestBucketUpdateRejectsUnsafeReplicationWire(t *testing.T) {
	source := AsReplicationSource{SourceApplicationKeyID: "source-key"}
	for _, tc := range []struct {
		name, response string
		config         *ReplicationConfiguration
		want           string
	}{
		{
			name: "unreadable source-only update", response: unreadableReplicationUpdateJSON,
			config: &ReplicationConfiguration{AsReplicationSource: source},
			want:   "b2: cannot update unreadable replication configuration",
		},
		{
			name: "unreadable destination update", response: unreadableReplicationUpdateJSON,
			config: &ReplicationConfiguration{AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"source-key": "destination-key"}}},
			want:   "b2: cannot update unreadable replication configuration",
		},
		{
			name: "empty without cached configuration", response: noReplicationBucketJSON,
			config: &ReplicationConfiguration{}, want: "b2: replication configuration requires a source or destination",
		},
		{
			name: "empty with cached source", response: sourceReplBucketJSON,
			config: &ReplicationConfiguration{}, want: "b2: replication configuration requires a source or destination",
		},
		{
			name: "empty with cached destination", response: destinationReplicationUpdateJSON,
			config: &ReplicationConfiguration{}, want: "b2: replication configuration requires a source or destination",
		},
		{
			name: "empty with both cached roles", response: bothReplBucketJSON,
			config: &ReplicationConfiguration{}, want: "b2: replication configuration requires a source or destination",
		},
		{
			name: "empty source rules", response: noReplicationBucketJSON,
			config: &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{ReplicationRules: []ReplicationRules{}}},
			want:   "b2: replication configuration requires a source or destination",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := replicationUpdateWireBucket(t, tc.response)
			before := len(srv.Requests())
			err := bucket.Update(context.Background(), &BucketAttrs{
				Type: Public, Info: map[string]string{"rejected": "value"}, ReplicationConfig: tc.config,
			})
			if err == nil || err.Error() != tc.want {
				t.Errorf("Update error = %v, want %q", err, tc.want)
			}
			if got := len(srv.Requests()) - before; got != 0 {
				t.Errorf("HTTP requests from rejected replication update = %d, want 0", got)
			}
			if err := bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"accepted": "value"}}); err != nil {
				t.Fatal(err)
			}
			request := srv.Last(t, "b2_update_bucket")
			b2wiretest.AssertJSON(t, request, "bucketType", "allPrivate")
			b2wiretest.AssertJSON(t, request, "bucketInfo", map[string]string{"accepted": "value"})
			b2wiretest.AssertNoJSON(t, request, "replicationConfiguration")
		})
	}
}

func TestBucketUpdateTracksUnreadableReplicationResponsesWire(t *testing.T) {
	for _, origin := range []string{"create", "update"} {
		t.Run(origin, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.Reply("b2_update_bucket", http.StatusOK, unreadableReplicationUpdateJSON)
			client := liveOnlyClient(t, srv)
			var bucket *Bucket
			var err error
			if origin == "create" {
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
				srv.Reply("b2_create_bucket", http.StatusOK, unreadableReplicationUpdateJSON)
				bucket, err = client.NewBucket(context.Background(), "n", &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
					AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"source-key": "destination-key"}},
				}})
			} else {
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+noReplicationBucketJSON+`]}`)
				bucket, err = client.Bucket(context.Background(), "n")
				if err == nil {
					err = bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"metadata": "changed"}})
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := len(srv.Requests())
			err = bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
				AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "source-key"},
			}})
			const want = "b2: cannot update unreadable replication configuration"
			if err == nil || err.Error() != want {
				t.Errorf("Update from unreadable %s response = %v, want %q", origin, err, want)
			}
			if got := len(srv.Requests()) - before; got != 0 {
				t.Errorf("HTTP requests from rejected replication update = %d, want 0", got)
			}
		})
	}
}

func TestBucketUpdateKeepsUnreadableReplicationStateWire(t *testing.T) {
	srv, bucket := replicationUpdateWireBucket(t, unreadableReplicationUpdateJSON)
	if err := bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"metadata": "changed"}}); err != nil {
		t.Fatal(err)
	}
	before := len(srv.Requests())
	err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
		AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "source-key"},
	}})
	const want = "b2: cannot update unreadable replication configuration"
	if err == nil || err.Error() != want {
		t.Errorf("Update after metadata-only update = %v, want %q", err, want)
	}
	if got := len(srv.Requests()) - before; got != 0 {
		t.Errorf("HTTP requests from unreadable replication update = %d, want 0", got)
	}
}

// https://www.backblaze.com/docs/cloud-storage-create-a-cloud-replication-rule-with-the-native-api
// defines map keys as source key IDs and values as destination key IDs.
func TestBucketUpdateReplacesDestinationMappingWire(t *testing.T) {
	srv, bucket := replicationUpdateWireBucket(t, destinationReplicationUpdateJSON)
	if err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{
			"source-key":       "new-destination-key",
			"other-source-key": "other-destination-key",
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	updates := srv.Calls("b2_update_bucket")
	if len(updates) != 1 {
		t.Fatalf("update requests = %d, want 1", len(updates))
	}
	b2wiretest.AssertJSON(t, updates[0], "bucketId", "bid")
	b2wiretest.AssertJSON(t, updates[0], "replicationConfiguration", map[string]any{
		"asReplicationDestination": map[string]any{"sourceToDestinationKeyMapping": map[string]string{
			"source-key":       "new-destination-key",
			"other-source-key": "other-destination-key",
		}},
	})
}

func TestNewBucketRejectsUnsafeReplicationWire(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *ReplicationConfiguration
		want   string
	}{
		{"empty configuration", &ReplicationConfiguration{}, "b2: replication configuration requires a source or destination"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
			srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
			client := liveOnlyClient(t, srv)
			before := len(srv.Requests())
			bucket, err := client.NewBucket(context.Background(), "n", &BucketAttrs{ReplicationConfig: tc.config})
			if bucket != nil || err == nil || err.Error() != tc.want {
				t.Errorf("NewBucket = %v, %v; want nil, %q", bucket, err, tc.want)
			}
			requests := srv.Requests()[before:]
			if len(requests) != 1 || requests[0].Call != "b2_list_buckets" {
				t.Errorf("invalid creation requests = %d, want only the existing-name lookup", len(requests))
			}
			if count := len(srv.Calls("b2_create_bucket")); count != 0 {
				t.Errorf("create requests = %d, want 0", count)
			}
		})
	}
}

func TestNewBucketExistingNameIgnoresReplicationAttrsWire(t *testing.T) {
	srv, bucket := replicationUpdateWireBucket(t, noReplicationBucketJSON)
	existing, err := bucket.c.NewBucket(context.Background(), "n", &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{}})
	if err != nil || existing == nil || existing.ID() != "bid" {
		t.Fatalf("NewBucket existing name = %v, %v; want existing bucket", existing, err)
	}
	if count := len(srv.Calls("b2_create_bucket")); count != 0 {
		t.Errorf("create requests for existing bucket = %d, want 0", count)
	}
}

func TestBucketUpdateAcceptsCompleteUnreadableReplacementWire(t *testing.T) {
	srv, bucket := replicationUpdateWireBucket(t, unreadableReplicationUpdateJSON)
	if err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: replicationCreateConfig()}); err != nil {
		t.Fatal(err)
	}
	updates := srv.Calls("b2_update_bucket")
	if len(updates) != 1 {
		t.Fatalf("complete replacement requests = %d, want 1", len(updates))
	}
	b2wiretest.AssertJSON(t, updates[0], "replicationConfiguration.asReplicationSource.sourceApplicationKeyId", "source-key")
	b2wiretest.AssertJSON(t, updates[0], "replicationConfiguration.asReplicationDestination.sourceToDestinationKeyMapping", map[string]string{"source-key": "destination-key"})
}

func TestBucketUpdateDestinationPreservesSourceWire(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []ReplicationRules
	}{{"nil source rules", nil}, {"empty source rules", []ReplicationRules{}}} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := replicationUpdateWireBucket(t, bothReplBucketJSON)
			if err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
				AsReplicationSource: AsReplicationSource{ReplicationRules: tc.rules},
				AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{
					"source-key": "new-destination-key",
				}},
			}}); err != nil {
				t.Fatal(err)
			}
			request := srv.Last(t, "b2_update_bucket")
			b2wiretest.AssertJSON(t, request, "replicationConfiguration.asReplicationSource", map[string]any{
				"sourceApplicationKeyId": "cached-key",
				"replicationRules": []map[string]any{{
					"destinationBucketId": "dest", "fileNamePrefix": "", "includeExistingFiles": false,
					"isEnabled": true, "priority": 1, "replicationRuleName": "cached-rule",
				}},
			})
			b2wiretest.AssertJSON(t, request, "replicationConfiguration.asReplicationDestination.sourceToDestinationKeyMapping", map[string]string{"source-key": "new-destination-key"})
		})
	}
}

// B2 requires sourceToDestinationKeyMapping in a destination object. The current
// encoder omits nil/empty maps; the service validates that representation.
// https://www.backblaze.com/apidocs/b2-update-bucket
// TestReplicationDestinationPartialUpdateLive pins the actual missing-field error.
func TestReplicationDestinationEmptyMapsUseServiceValidationWire(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name       string
				mapping    map[string]string
				withSource bool
			}{
				{name: "nil map"},
				{name: "empty map", mapping: map[string]string{}},
				{name: "source plus nil map", withSource: true},
				{name: "source plus empty map", mapping: map[string]string{}, withSource: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv := b2wiretest.New(t)
					call := "b2_create_bucket"
					listed := `{"buckets":[]}`
					if operation == "update" {
						call = "b2_update_bucket"
						listed = `{"buckets":[` + destinationReplicationUpdateJSON + `]}`
					}
					srv.Reply("b2_list_buckets", http.StatusOK, listed)
					srv.StrictReplySequence(call, b2wiretest.Response{
						Status: http.StatusBadRequest,
						Body:   `{"status":400,"code":"bad_request","message":"missing required field: sourceToDestinationKeyMapping"}`,
					})
					client := liveOnlyClient(t, srv)
					config := &ReplicationConfiguration{
						AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: tc.mapping},
					}
					want := map[string]any{"asReplicationDestination": map[string]any{}}
					if tc.withSource {
						config.AsReplicationSource = replicationCreateConfig().AsReplicationSource
						want["asReplicationSource"] = map[string]any{
							"sourceApplicationKeyId": "source-key",
							"replicationRules": []map[string]any{{
								"destinationBucketId": "destination-bucket-id", "fileNamePrefix": "prefix/",
								"includeExistingFiles": true, "isEnabled": false, "priority": 1,
								"replicationRuleName": "disabled-rule",
							}},
						}
					}
					var err error
					if operation == "create" {
						_, err = client.NewBucket(context.Background(), "n", &BucketAttrs{ReplicationConfig: config})
					} else {
						bucket, lookupErr := client.Bucket(context.Background(), "n")
						if lookupErr != nil {
							t.Fatal(lookupErr)
						}
						err = bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: config})
					}
					status, code, message := base.MsgCode(err)
					if status != 400 || code != "bad_request" || message != "missing required field: sourceToDestinationKeyMapping" {
						t.Errorf("%s error = %v, want the service's 400 bad_request missing-field error", operation, err)
					}
					calls := srv.Calls(call)
					if len(calls) != 1 {
						t.Fatalf("%s requests = %d, want 1", call, len(calls))
					}
					b2wiretest.AssertJSON(t, calls[0], "replicationConfiguration", want)
				})
			}
		})
	}
}

func TestFailedBucketUpdateRestoresCachedReplicationWire(t *testing.T) {
	srv, bucket := replicationUpdateWireBucket(t, bothReplBucketJSON)
	// A terminal 5xx exercises rollback without waiting through retry backoff.
	srv.StrictReplySequence("b2_update_bucket",
		b2wiretest.Response{Status: http.StatusNotImplemented, Body: `{"status":501,"code":"not_implemented","message":"scripted rejection"}`},
		b2wiretest.Response{Body: bothReplBucketJSON},
	)
	err := bucket.Update(context.Background(), &BucketAttrs{Info: map[string]string{"metadata": "rejected"}})
	if status, code, message := base.MsgCode(err); status != 501 || code != "not_implemented" || message != "scripted rejection" {
		t.Fatalf("metadata update error = %v, want scripted 501 not_implemented", err)
	}
	if err := bucket.Update(context.Background(), &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
		AsReplicationSource: replicationCreateConfig().AsReplicationSource,
	}}); err != nil {
		t.Fatal(err)
	}
	updates := srv.Calls("b2_update_bucket")
	if len(updates) != 2 {
		t.Fatalf("update requests = %d, want 2", len(updates))
	}
	b2wiretest.AssertNoJSON(t, updates[0], "replicationConfiguration")
	b2wiretest.AssertJSON(t, updates[1], "replicationConfiguration.asReplicationSource.sourceApplicationKeyId", "source-key")
	b2wiretest.AssertJSON(t, updates[1], "replicationConfiguration.asReplicationDestination.sourceToDestinationKeyMapping", map[string]string{
		"source-key": "destination-key",
	})
}
