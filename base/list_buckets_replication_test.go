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
	"testing"
)

func TestListBucketsReplicationConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		canRead    bool
		wantConfig bool
	}{
		{name: "authorized", canRead: true, wantConfig: true},
		{name: "unauthorized", canRead: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucketJSON := fmt.Sprintf(`{"bucketId":"bid","bucketName":"name","bucketType":"allPrivate","replicationConfiguration":{"isClientAuthorizedToRead":%t,"value":{"asReplicationDestination":{"sourceToDestinationKeyMapping":{"source-key":"destination-key"}}}}}`, tc.canRead)
			b, _, closeSrv := bucketFixture(t, bucketJSON)
			defer closeSrv()

			buckets, err := b.ListBuckets(context.Background(), "")
			if err != nil {
				t.Fatalf("ListBuckets: %v", err)
			}
			if len(buckets) != 1 {
				t.Fatalf("ListBuckets returned %d buckets, want 1", len(buckets))
			}
			cfg := buckets[0].ReplicationConfiguration
			if !tc.wantConfig {
				if cfg != nil {
					t.Errorf("unauthorized replication configuration = %+v, want nil", cfg)
				}
				return
			}
			if cfg == nil || cfg.AsReplicationDestination == nil || cfg.AsReplicationDestination.SourceToDestinationKeyMapping["source-key"] != "destination-key" {
				t.Errorf("replication configuration = %+v, want destination key mapping", cfg)
			}
		})
	}
}
