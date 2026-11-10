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
		{
			name: "nil destination map", response: destinationReplicationUpdateJSON,
			config: &ReplicationConfiguration{AsReplicationDestination: &AsReplicationDestination{}},
			want:   "b2: replication destination requires a non-empty key mapping",
		},
		{
			name: "empty destination map", response: destinationReplicationUpdateJSON,
			config: &ReplicationConfiguration{AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{}}},
			want:   "b2: replication destination requires a non-empty key mapping",
		},
		{
			name: "source with nil destination map", response: bothReplBucketJSON,
			config: &ReplicationConfiguration{AsReplicationSource: source, AsReplicationDestination: &AsReplicationDestination{}},
			want:   "b2: replication destination requires a non-empty key mapping",
		},
		{
			name: "source with empty destination map", response: bothReplBucketJSON,
			config: &ReplicationConfiguration{AsReplicationSource: source, AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{}}},
			want:   "b2: replication destination requires a non-empty key mapping",
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
