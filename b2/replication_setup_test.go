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
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	if !reflect.DeepEqual(source.AsReplicationSource.ReplicationRules, []ReplicationRules{oldRule, newRule}) || source.AsReplicationSource.SourceApplicationKeyID != "old-src-key" {
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
	config      *ReplicationConfiguration
	unreadable  bool
	updateErr   error
	updateOrder *[]string
	updates     []*BucketAttrs
}

func (b *replicationTestBucket) attrs() *BucketAttrs {
	return &BucketAttrs{Type: Private, ReplicationConfig: copyReplicationConfiguration(b.config), replicationConfigUnreadable: b.unreadable}
}

func (b *replicationTestBucket) updateBucket(_ context.Context, attrs *BucketAttrs) error {
	if b.updateOrder != nil {
		*b.updateOrder = append(*b.updateOrder, b.n)
	}
	if b.updateErr != nil {
		err := b.updateErr
		b.updateErr = nil
		return err
	}
	b.config = copyReplicationConfiguration(attrs.ReplicationConfig)
	b.updates = append(b.updates, &BucketAttrs{ReplicationConfig: copyReplicationConfiguration(attrs.ReplicationConfig)})
	return nil
}

type replicationTestRoot struct {
	*testRoot
	buckets  map[string]*replicationTestBucket
	keyCalls int
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
	r.keyCalls++
	return nil, fmt.Errorf("unexpected key creation")
}

func TestSetupReplicationUpdatesBothBuckets(t *testing.T) {
	ctx := context.Background()
	root := &replicationTestRoot{testRoot: &testRoot{errs: &errCont{}}}
	var updateOrder []string
	srcBackend := &replicationTestBucket{testBucket: &testBucket{n: "source", idv: "src-id", errs: &errCont{}}, config: &ReplicationConfiguration{
		AsReplicationSource:      AsReplicationSource{SourceApplicationKeyID: "previous-key", ReplicationRules: []ReplicationRules{{ReplicationRuleName: "previous", DestinationBucketID: "other-id", Priority: 40}}},
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"old": "mapped"}},
	}, updateOrder: &updateOrder}
	dstBackend := &replicationTestBucket{testBucket: &testBucket{n: "destination", idv: "dst-id", errs: &errCont{}}, config: &ReplicationConfiguration{
		AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"another": "key"}},
	}, updateOrder: &updateOrder}
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
	options := []ReplicationOption{WithRuleName("new-rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true)}
	if err := SetupReplication(ctx, src, dst, "previous-key", "destination-key", options...); err != nil {
		t.Fatal(err)
	}
	if root.keyCalls != 0 {
		t.Fatalf("SetupReplication created %d keys, want none", root.keyCalls)
	}
	if len(dstBackend.updates) != 1 || len(srcBackend.updates) != 1 {
		t.Fatalf("update counts: src=%d dst=%d", len(srcBackend.updates), len(dstBackend.updates))
	}
	if !reflect.DeepEqual(updateOrder, []string{"destination", "source"}) {
		t.Fatalf("update order = %v, want destination before source", updateOrder)
	}
	if got := dstBackend.updates[0].ReplicationConfig.AsReplicationDestination.SourceToDestinationKeyMapping; !reflect.DeepEqual(got, map[string]string{"another": "key", "previous-key": "destination-key"}) {
		t.Fatalf("destination update mapping = %#v", got)
	}
	srcCfg := srcBackend.updates[0].ReplicationConfig
	if srcCfg.AsReplicationSource.SourceApplicationKeyID != "previous-key" || len(srcCfg.AsReplicationSource.ReplicationRules) != 2 || srcCfg.AsReplicationSource.ReplicationRules[0].ReplicationRuleName != "previous" || srcCfg.AsReplicationDestination.SourceToDestinationKeyMapping["old"] != "mapped" {
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
	if err := SetupReplication(ctx, src, dst, "previous-key", "destination-key", options...); err != nil {
		t.Fatal(err)
	}
	if root.keyCalls != 0 || len(srcBackend.updates) != 1 || len(dstBackend.updates) != 1 {
		t.Fatalf("rerun duplicated setup: key calls=%d src updates=%d dst updates=%d", root.keyCalls, len(srcBackend.updates), len(dstBackend.updates))
	}
}

func TestSetupReplicationReusesExistingSourceKey(t *testing.T) {
	ctx := context.Background()
	root := &replicationTestRoot{testRoot: &testRoot{errs: &errCont{}}}
	srcBackend := &replicationTestBucket{testBucket: &testBucket{n: "source", idv: "src-id", errs: &errCont{}}, config: &ReplicationConfiguration{
		AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "K1", ReplicationRules: []ReplicationRules{{ReplicationRuleName: "to-D1", DestinationBucketID: "D1", Priority: 10}}},
	}}
	dstBackend := &replicationTestBucket{testBucket: &testBucket{n: "destination", idv: "D2", errs: &errCont{}}}
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
	if err := SetupReplication(ctx, src, dst, "K1", "D2-key", WithRuleName("to-D2")); err != nil {
		t.Fatal(err)
	}
	if got := srcBackend.config.AsReplicationSource.SourceApplicationKeyID; got != "K1" {
		t.Fatalf("source key ID = %q, want K1", got)
	}
	if rules := srcBackend.config.AsReplicationSource.ReplicationRules; len(rules) != 2 || rules[0].DestinationBucketID != "D1" || rules[1].DestinationBucketID != "D2" {
		t.Fatalf("source rules = %+v", rules)
	}
	if root.keyCalls != 0 {
		t.Fatalf("created %d keys, want none", root.keyCalls)
	}
	if got := dstBackend.config.AsReplicationDestination.SourceToDestinationKeyMapping["K1"]; got != "D2-key" {
		t.Fatalf("D2 mapping for K1 = %q, want D2-key", got)
	}
}

func replicationTestPair(t *testing.T, sourceConfig, destinationConfig *ReplicationConfiguration) (*replicationTestRoot, *replicationTestBucket, *replicationTestBucket, *Bucket, *Bucket) {
	t.Helper()
	root := &replicationTestRoot{testRoot: &testRoot{errs: &errCont{}}}
	sourceBackend := &replicationTestBucket{testBucket: &testBucket{n: "source", idv: "src-id", errs: &errCont{}}, config: sourceConfig}
	destinationBackend := &replicationTestBucket{testBucket: &testBucket{n: "destination", idv: "dst-id", errs: &errCont{}}, config: destinationConfig}
	root.buckets = map[string]*replicationTestBucket{"source": sourceBackend, "destination": destinationBackend}
	client := &Client{backend: &beRoot{b2i: root}}
	source, err := client.Bucket(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := client.Bucket(context.Background(), "destination")
	if err != nil {
		t.Fatal(err)
	}
	return root, sourceBackend, destinationBackend, source, destination
}

func TestSetupReplicationSourceKeyConflict(t *testing.T) {
	cfg := &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "existing-source-key"}}
	root, srcBackend, dstBackend, src, dst := replicationTestPair(t, cfg, nil)
	err := SetupReplication(context.Background(), src, dst, "different-source-key", "destination-key")
	if err == nil || !strings.Contains(err.Error(), "different application key ID") {
		t.Fatalf("SetupReplication() = %v, want source-key conflict", err)
	}
	if root.keyCalls != 0 || len(srcBackend.updates) != 0 || len(dstBackend.updates) != 0 {
		t.Fatalf("conflicting key caused changes: key calls=%d source updates=%d destination updates=%d", root.keyCalls, len(srcBackend.updates), len(dstBackend.updates))
	}
}

func TestSetupReplicationIdempotency(t *testing.T) {
	for _, tc := range []struct {
		name             string
		options          []ReplicationOption
		configuredDstID  string
		destinationKeyID string
		wantErr          bool
	}{
		{name: "exact match", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true), WithRulePriority(17)}, destinationKeyID: "destination-key"},
		{name: "exact match without explicit priority", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true)}, destinationKeyID: "destination-key"},
		{name: "different prefix", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("other/"), WithIncludeExistingFiles(true)}, destinationKeyID: "destination-key", wantErr: true},
		{name: "different include-existing option", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/")}, destinationKeyID: "destination-key", wantErr: true},
		{name: "different priority option", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true), WithRulePriority(18)}, destinationKeyID: "destination-key", wantErr: true},
		{name: "different destination", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true)}, configuredDstID: "other-dst", destinationKeyID: "destination-key", wantErr: true},
		{name: "different destination key mapping", options: []ReplicationOption{WithRuleName("rule"), WithFileNamePrefix("logs/"), WithIncludeExistingFiles(true)}, destinationKeyID: "other-key", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dstID := tc.configuredDstID
			if dstID == "" {
				dstID = "dst-id"
			}
			sourceConfig := &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "source-key", ReplicationRules: []ReplicationRules{{ReplicationRuleName: "rule", DestinationBucketID: dstID, FileNamePrefix: "logs/", IncludeExistingFiles: true, IsEnabled: true, Priority: 17}}}}
			destinationConfig := &ReplicationConfiguration{AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"source-key": "destination-key"}}}
			root, srcBackend, dstBackend, src, dst := replicationTestPair(t, sourceConfig, destinationConfig)
			err := SetupReplication(context.Background(), src, dst, "source-key", tc.destinationKeyID, tc.options...)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SetupReplication() = %v, wantErr=%t", err, tc.wantErr)
			}
			if root.keyCalls != 0 || len(srcBackend.updates) != 0 || len(dstBackend.updates) != 0 {
				t.Fatalf("repeat changed state: key calls=%d source updates=%d destination updates=%d", root.keyCalls, len(srcBackend.updates), len(dstBackend.updates))
			}
		})
	}
}

func TestSetupReplicationFailsClosedOnUnreadableConfig(t *testing.T) {
	for _, hidden := range []string{"source", "destination"} {
		t.Run(hidden, func(t *testing.T) {
			root, srcBackend, dstBackend, src, dst := replicationTestPair(t, nil, nil)
			if hidden == "source" {
				srcBackend.unreadable = true
			} else {
				dstBackend.unreadable = true
			}
			err := SetupReplication(context.Background(), src, dst, "source-key", "destination-key")
			if err == nil || !strings.Contains(err.Error(), "cannot read existing replication config to merge safely") {
				t.Fatalf("SetupReplication() = %v, want unreadable-config error", err)
			}
			if root.keyCalls != 0 || len(srcBackend.updates) != 0 || len(dstBackend.updates) != 0 {
				t.Fatalf("hidden config caused changes: key calls=%d source updates=%d destination updates=%d", root.keyCalls, len(srcBackend.updates), len(dstBackend.updates))
			}
		})
	}
}

func TestReplicationConfigReadabilitySignal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bucketJSON string
		wantHidden bool
	}{
		{name: "hidden existing config", bucketJSON: filteredReplBucketJSON, wantHidden: true},
		{name: "readable absent config", bucketJSON: noReplicationBucketJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newListReplFixture(t, tc.bucketJSON, false)
			buckets, err := client.ListBuckets(context.Background())
			if err != nil || len(buckets) != 1 {
				t.Fatalf("ListBuckets() = %v, %v, want one bucket", buckets, err)
			}
			attrs, err := buckets[0].Attrs(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if attrs.replicationConfigUnreadable != tc.wantHidden {
				t.Fatalf("unreadable=%t, want %t", attrs.replicationConfigUnreadable, tc.wantHidden)
			}
		})
	}
}

func TestSetupReplicationCompletesAfterSourceUpdateError(t *testing.T) {
	root, srcBackend, dstBackend, src, dst := replicationTestPair(t, nil, nil)
	srcBackend.updateErr = errors.New("source update failed")
	options := []ReplicationOption{WithRuleName("rule")}
	if err := SetupReplication(context.Background(), src, dst, "source-key", "destination-key", options...); err == nil {
		t.Fatal("SetupReplication() = nil, want source update error")
	}
	if len(dstBackend.updates) != 1 || dstBackend.config.AsReplicationDestination.SourceToDestinationKeyMapping["source-key"] != "destination-key" || len(srcBackend.updates) != 0 {
		t.Fatalf("partial setup = destination %+v, source %+v", dstBackend.config, srcBackend.config)
	}
	if err := SetupReplication(context.Background(), src, dst, "source-key", "destination-key", options...); err != nil {
		t.Fatal(err)
	}
	if root.keyCalls != 0 || len(srcBackend.config.AsReplicationSource.ReplicationRules) != 1 || dstBackend.config.AsReplicationDestination.SourceToDestinationKeyMapping["source-key"] != "destination-key" {
		t.Fatalf("retry did not finish setup: keys=%d source=%+v destination=%+v", root.keyCalls, srcBackend.config, dstBackend.config)
	}
}

func TestSetupReplicationRepairsMissingDestinationMapping(t *testing.T) {
	sourceConfig := &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{
		SourceApplicationKeyID: "source-key",
		ReplicationRules:       []ReplicationRules{{ReplicationRuleName: "rule", DestinationBucketID: "dst-id", IsEnabled: true, Priority: 17}},
	}}
	root, srcBackend, dstBackend, src, dst := replicationTestPair(t, sourceConfig, nil)
	if err := SetupReplication(context.Background(), src, dst, "source-key", "destination-key", WithRuleName("rule")); err != nil {
		t.Fatal(err)
	}
	if root.keyCalls != 0 || len(srcBackend.updates) != 0 || len(dstBackend.updates) != 1 || dstBackend.config.AsReplicationDestination.SourceToDestinationKeyMapping["source-key"] != "destination-key" {
		t.Fatalf("missing mapping repair: key calls=%d source updates=%d destination=%+v", root.keyCalls, len(srcBackend.updates), dstBackend.config)
	}
}

func TestSetupReplicationExplicitPriority(t *testing.T) {
	root, srcBackend, dstBackend, src, dst := replicationTestPair(t, nil, nil)
	if err := SetupReplication(context.Background(), src, dst, "source-key", "destination-key", WithRuleName("rule"), WithRulePriority(77)); err != nil {
		t.Fatal(err)
	}
	rules := srcBackend.config.AsReplicationSource.ReplicationRules
	if root.keyCalls != 0 || len(rules) != 1 || rules[0].Priority != 77 || srcBackend.config.AsReplicationSource.SourceApplicationKeyID != "source-key" || dstBackend.config.AsReplicationDestination.SourceToDestinationKeyMapping["source-key"] != "destination-key" {
		t.Fatalf("explicit priority setup: key calls=%d source=%+v destination=%+v", root.keyCalls, srcBackend.config, dstBackend.config)
	}
}
