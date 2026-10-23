package b2

import "testing"

func FuzzReplicationMerge(f *testing.F) {
	f.Add("source", "destination", int64(128))
	f.Add("", "", int64(2147483647))
	f.Fuzz(func(t *testing.T, source, destination string, priority int64) {
		priority &= 0x7fffffff
		cfg := &ReplicationConfiguration{AsReplicationDestination: &AsReplicationDestination{SourceToDestinationKeyMapping: map[string]string{"existing": "preserved"}}}
		cfg.AsReplicationSource.ReplicationRules = []ReplicationRules{{Priority: int(priority), ReplicationRuleName: "existing"}}
		merged := mergeReplicationDestination(cfg, source, destination)
		if cfg.AsReplicationDestination.SourceToDestinationKeyMapping["existing"] != "preserved" || len(cfg.AsReplicationDestination.SourceToDestinationKeyMapping) != 1 {
			t.Fatal("input mapping mutated")
		}
		if merged.AsReplicationDestination.SourceToDestinationKeyMapping[source] != destination {
			t.Fatal("mapping lost")
		}
		next := nextReplicationPriority(cfg)
		if next < int(priority) || next > maxReplicationPriority {
			t.Fatalf("priority=%d next=%d", priority, next)
		}
	})
}
