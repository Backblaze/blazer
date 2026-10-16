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
	"reflect"
	"testing"
	"time"
)

func TestNextReplicationPriority(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  *ReplicationConfiguration
		want int
	}{
		{name: "empty", want: defaultReplicationPriority},
		{name: "with existing", cfg: &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{ReplicationRules: []ReplicationRules{{Priority: 100}, {Priority: 140}}}}, want: 145},
		{name: "cap", cfg: &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{ReplicationRules: []ReplicationRules{{Priority: maxReplicationPriority - 2}}}}, want: maxReplicationPriority},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nextReplicationPriority(test.cfg); got != test.want {
				t.Fatalf("nextReplicationPriority() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestMergeReplicationConfiguration(t *testing.T) {
	oldRule := ReplicationRules{ReplicationRuleName: "old", DestinationBucketID: "old-dst", Priority: 10}
	original := &ReplicationConfiguration{
		AsReplicationSource:      AsReplicationSource{SourceApplicationKeyID: "old-src-key", ReplicationRules: []ReplicationRules{oldRule}},
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"old-src-key": "old-dst-key"}},
	}
	newRule := ReplicationRules{ReplicationRuleName: "new", DestinationBucketID: "new-dst", Priority: 15}
	source := mergeReplicationSource(original, "new-src-key", newRule)
	destination := mergeReplicationDestination(original, "new-src-key", "new-dst-key")
	if !reflect.DeepEqual(source.AsReplicationSource.ReplicationRules, []ReplicationRules{oldRule, newRule}) || source.AsReplicationSource.SourceApplicationKeyID != "new-src-key" {
		t.Fatalf("merged source = %+v", source.AsReplicationSource)
	}
	if !reflect.DeepEqual(destination.AsReplicationDestination.SourceToDestinationKeyMapping, map[string]string{"old-src-key": "old-dst-key", "new-src-key": "new-dst-key"}) {
		t.Fatalf("merged destination = %+v", destination.AsReplicationDestination)
	}
	if original.AsReplicationSource.SourceApplicationKeyID != "old-src-key" || len(original.AsReplicationSource.ReplicationRules) != 1 || len(original.AsReplicationDestination.SourceToDestinationKeyMapping) != 1 {
		t.Fatalf("merge changed original: %+v", original)
	}
}

type replicationTestBucket struct {
	*testBucket
	config  *ReplicationConfiguration
	updates []*BucketAttrs
}

func (b *replicationTestBucket) attrs() *BucketAttrs {
	return &BucketAttrs{Type: Private, ReplicationConfig: copyReplicationConfiguration(b.config)}
}

func (b *replicationTestBucket) updateBucket(_ context.Context, attrs *BucketAttrs) error {
	b.config = copyReplicationConfiguration(attrs.ReplicationConfig)
	b.updates = append(b.updates, &BucketAttrs{ReplicationConfig: copyReplicationConfiguration(attrs.ReplicationConfig)})
	return nil
}

type replicationTestRoot struct {
	*testRoot
	buckets map[string]*replicationTestBucket
	keys    []replicationTestKey
}

func (r *replicationTestRoot) listBuckets(_ context.Context, name string, _ ...string) ([]b2BucketInterface, error) {
	if name != "" {
		if bucket := r.buckets[name]; bucket != nil {
			return []b2BucketInterface{bucket}, nil
		}
		return nil, nil
	}
	var result []b2BucketInterface
	for _, bucket := range r.buckets {
		result = append(result, bucket)
	}
	return result, nil
}

func (r *replicationTestRoot) createKey(_ context.Context, name string, caps []string, _ time.Duration, bucketID, prefix string) (b2KeyInterface, error) {
	key := replicationTestKey{nameValue: name, idValue: fmt.Sprintf("key-%d", len(r.keys)+1), capsValue: append([]string(nil), caps...), bucketID: bucketID, prefix: prefix}
	r.keys = append(r.keys, key)
	return &key, nil
}

type replicationTestKey struct {
	nameValue, idValue, bucketID, prefix string
	capsValue                            []string
}

func (*replicationTestKey) del(context.Context) error { return nil }
func (k *replicationTestKey) caps() []string          { return k.capsValue }
func (k *replicationTestKey) name() string            { return k.nameValue }
func (*replicationTestKey) expires() time.Time        { return time.Time{} }
func (*replicationTestKey) secret() string            { return "secret" }
func (k *replicationTestKey) id() string              { return k.idValue }

func TestSetupReplicationUpdatesBothBuckets(t *testing.T) {
	ctx := context.Background()
	root := &replicationTestRoot{testRoot: &testRoot{errs: &errCont{}}}
	srcBackend := &replicationTestBucket{testBucket: &testBucket{n: "source", idv: "src-id", errs: &errCont{}}, config: &ReplicationConfiguration{
		AsReplicationSource:      AsReplicationSource{SourceApplicationKeyID: "previous-key", ReplicationRules: []ReplicationRules{{ReplicationRuleName: "previous", DestinationBucketID: "other-id", Priority: 40}}},
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"old": "mapped"}},
	}}
	dstBackend := &replicationTestBucket{testBucket: &testBucket{n: "destination", idv: "dst-id", errs: &errCont{}}, config: &ReplicationConfiguration{
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"another": "key"}},
	}}
	root.buckets = map[string]*replicationTestBucket{"source": srcBackend, "destination": dstBackend}
	client := &Client{backend: &beRoot{b2i: root}}
	src, err := client.Bucket(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := client.Bucket(ctx, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetupReplication(ctx, src, dst, WithRuleName("new-rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true)); err != nil {
		t.Fatal(err)
	}
	if len(root.keys) != 2 || root.keys[0].bucketID != "src-id" || root.keys[1].bucketID != "dst-id" || root.keys[0].prefix != "" || root.keys[1].prefix != "" {
		t.Fatalf("created keys = %+v", root.keys)
	}
	if !reflect.DeepEqual(root.keys[0].capsValue, []string{"readFiles", "readFileLegalHolds", "readFileRetentions"}) || !reflect.DeepEqual(root.keys[1].capsValue, []string{"writeFiles", "writeFileLegalHolds", "writeFileRetentions", "deleteFiles"}) {
		t.Fatalf("key capabilities = %+v", root.keys)
	}
	if len(dstBackend.updates) != 1 || len(srcBackend.updates) != 1 {
		t.Fatalf("update counts: src=%d dst=%d", len(srcBackend.updates), len(dstBackend.updates))
	}
	if got := dstBackend.updates[0].ReplicationConfig.AsReplicationDestination.SourceToDestinationKeyMapping; !reflect.DeepEqual(got, map[string]string{"another": "key", "key-1": "key-2"}) {
		t.Fatalf("destination update mapping = %#v", got)
	}
	srcCfg := srcBackend.updates[0].ReplicationConfig
	if srcCfg.AsReplicationSource.SourceApplicationKeyID != "key-1" || len(srcCfg.AsReplicationSource.ReplicationRules) != 2 || srcCfg.AsReplicationSource.ReplicationRules[0].ReplicationRuleName != "previous" || srcCfg.AsReplicationDestination.SourceToDestinationKeyMapping["old"] != "mapped" {
		t.Fatalf("source update = %+v", srcCfg)
	}
	rule := srcCfg.AsReplicationSource.ReplicationRules[1]
	if rule.DestinationBucketID != "dst-id" || rule.ReplicationRuleName != "new-rule" || rule.FileNamePrefix != "logs/" || !rule.IncludeExistingFiles || !rule.IsEnabled || rule.Priority != 45 {
		t.Fatalf("new rule = %+v", rule)
	}
	status, err := ReplicationStatus(ctx, src)
	if err != nil || len(status) != 2 || !status[1].IsEnabled {
		t.Fatalf("ReplicationStatus = %+v, %v", status, err)
	}
	if err := SetupReplication(ctx, src, dst, WithRuleName("new-rule"), WithFileNamePrefix("logs/")); err != nil {
		t.Fatal(err)
	}
	if len(root.keys) != 2 || len(srcBackend.updates) != 1 || len(dstBackend.updates) != 1 {
		t.Fatalf("rerun duplicated setup: keys=%d src updates=%d dst updates=%d", len(root.keys), len(srcBackend.updates), len(dstBackend.updates))
	}
}
