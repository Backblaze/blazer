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
	"crypto/sha1"
	"fmt"
	"time"
)

const (
	defaultReplicationPriority = 128
	replicationPriorityOffset  = 5
	maxReplicationPriority     = 1<<31 - 1
)

type replicationOptions struct {
	name            string
	priority        int
	prioritySet     bool
	prefix          string
	includeExisting bool
}

// ReplicationOption configures SetupReplication.
type ReplicationOption func(*replicationOptions)

// WithRuleName names the new replication rule.
func WithRuleName(name string) ReplicationOption {
	return func(o *replicationOptions) { o.name = name }
}

// WithRulePriority sets the new rule's priority.
func WithRulePriority(priority int) ReplicationOption {
	return func(o *replicationOptions) { o.priority, o.prioritySet = priority, true }
}

// WithFileNamePrefix limits the rule to files whose names begin with prefix.
func WithFileNamePrefix(prefix string) ReplicationOption {
	return func(o *replicationOptions) { o.prefix = prefix }
}

// WithIncludeExistingFiles includes files already in the source bucket.
func WithIncludeExistingFiles(include bool) ReplicationOption {
	return func(o *replicationOptions) { o.includeExisting = include }
}

// ReplicationRuleStatus describes one configured rule and its enabled state.
// It does not report per-file replication progress.
type ReplicationRuleStatus struct{ ReplicationRules }

// ReplicationStatus returns the source bucket's configured replication rules.
func ReplicationStatus(ctx context.Context, src *Bucket) ([]ReplicationRuleStatus, error) {
	if src == nil {
		return nil, fmt.Errorf("replication status: source bucket is nil")
	}
	attrs, err := src.Attrs(ctx)
	if err != nil {
		return nil, err
	}
	if attrs.ReplicationConfig == nil {
		return nil, nil
	}
	var status []ReplicationRuleStatus
	for _, rule := range attrs.ReplicationConfig.AsReplicationSource.ReplicationRules {
		status = append(status, ReplicationRuleStatus{ReplicationRules: rule})
	}
	return status, nil
}

// SetupReplication configures one-way replication from src to dst. A repeated
// call for the same rule name, destination and prefix leaves it unchanged.
func SetupReplication(ctx context.Context, src, dst *Bucket, opts ...ReplicationOption) error {
	if src == nil || dst == nil {
		return fmt.Errorf("setup replication: source and destination buckets are required")
	}
	if src.ID() == dst.ID() {
		return fmt.Errorf("setup replication: source and destination must differ")
	}
	settings := replicationOptions{}
	for _, opt := range opts {
		opt(&settings)
	}
	if settings.prioritySet && (settings.priority < 1 || settings.priority > maxReplicationPriority) {
		return fmt.Errorf("setup replication: priority must be between 1 and %d", maxReplicationPriority)
	}
	if settings.name == "" {
		settings.name = defaultReplicationRuleName(dst.ID(), settings.prefix)
	}

	srcAttrs, err := src.Attrs(ctx)
	if err != nil {
		return err
	}
	dstAttrs, err := dst.Attrs(ctx)
	if err != nil {
		return err
	}
	if cfg := srcAttrs.ReplicationConfig; cfg != nil {
		for _, rule := range cfg.AsReplicationSource.ReplicationRules {
			if rule.ReplicationRuleName != settings.name {
				continue
			}
			if rule.DestinationBucketID == dst.ID() && rule.FileNamePrefix == settings.prefix {
				return nil
			}
			return fmt.Errorf("setup replication: rule name %q is already in use", settings.name)
		}
	}

	stamp := time.Now().UnixNano()
	sourceKeyID := ""
	if srcAttrs.ReplicationConfig != nil {
		sourceKeyID = srcAttrs.ReplicationConfig.AsReplicationSource.SourceApplicationKeyID
	}
	if sourceKeyID == "" {
		srcKey, err := src.CreateKey(ctx, fmt.Sprintf("%.60s-replsrc-%x", src.Name(), stamp),
			Capabilities("readFiles", "readFileLegalHolds", "readFileRetentions"))
		if err != nil {
			return err
		}
		sourceKeyID = srcKey.ID()
	}
	dstKey, err := dst.CreateKey(ctx, fmt.Sprintf("%.60s-repldst-%x", dst.Name(), stamp),
		Capabilities("writeFiles", "writeFileLegalHolds", "writeFileRetentions", "deleteFiles"))
	if err != nil {
		return err
	}
	priority := nextReplicationPriority(srcAttrs.ReplicationConfig)
	if settings.prioritySet {
		priority = settings.priority
	}
	rule := ReplicationRules{
		DestinationBucketID:  dst.ID(),
		FileNamePrefix:       settings.prefix,
		IncludeExistingFiles: settings.includeExisting,
		IsEnabled:            true,
		Priority:             priority,
		ReplicationRuleName:  settings.name,
	}
	if err := dst.Update(ctx, &BucketAttrs{ReplicationConfig: mergeReplicationDestination(dstAttrs.ReplicationConfig, sourceKeyID, dstKey.ID())}); err != nil {
		return err
	}
	return src.Update(ctx, &BucketAttrs{ReplicationConfig: mergeReplicationSource(srcAttrs.ReplicationConfig, sourceKeyID, rule)})
}

func defaultReplicationRuleName(destinationID, prefix string) string {
	hash := sha1.Sum([]byte(prefix))
	return fmt.Sprintf("replicate-%s-%x", destinationID, hash[:4])
}

func nextReplicationPriority(cfg *ReplicationConfiguration) int {
	if cfg == nil || len(cfg.AsReplicationSource.ReplicationRules) == 0 {
		return defaultReplicationPriority
	}
	max := 0
	for _, rule := range cfg.AsReplicationSource.ReplicationRules {
		if rule.Priority > max {
			max = rule.Priority
		}
	}
	if max >= maxReplicationPriority-replicationPriorityOffset {
		return maxReplicationPriority
	}
	return max + replicationPriorityOffset
}

func mergeReplicationDestination(cfg *ReplicationConfiguration, sourceKeyID, destinationKeyID string) *ReplicationConfiguration {
	merged := copyReplicationConfiguration(cfg)
	if merged.AsReplicationDestination == nil {
		merged.AsReplicationDestination = &AsReplicationDestination{SourceToDestinationKeyMapping: make(map[string]string)}
	}
	merged.AsReplicationDestination.SourceToDestinationKeyMapping[sourceKeyID] = destinationKeyID
	return merged
}

func mergeReplicationSource(cfg *ReplicationConfiguration, sourceKeyID string, rule ReplicationRules) *ReplicationConfiguration {
	merged := copyReplicationConfiguration(cfg)
	if merged.AsReplicationSource.SourceApplicationKeyID == "" {
		merged.AsReplicationSource.SourceApplicationKeyID = sourceKeyID
	}
	merged.AsReplicationSource.ReplicationRules = append(merged.AsReplicationSource.ReplicationRules, rule)
	return merged
}

func copyReplicationConfiguration(cfg *ReplicationConfiguration) *ReplicationConfiguration {
	copy := &ReplicationConfiguration{}
	if cfg == nil {
		return copy
	}
	copy.AsReplicationSource = cfg.AsReplicationSource
	copy.AsReplicationSource.ReplicationRules = append([]ReplicationRules(nil), cfg.AsReplicationSource.ReplicationRules...)
	if dest := cfg.AsReplicationDestination; dest != nil {
		mapping := make(map[string]string, len(dest.SourceToDestinationKeyMapping)+1)
		for source, destination := range dest.SourceToDestinationKeyMapping {
			mapping[source] = destination
		}
		copy.AsReplicationDestination = &AsReplicationDestination{SourceToDestinationKeyMapping: mapping}
	}
	return copy
}
