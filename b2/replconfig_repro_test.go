package b2

import (
	"context"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2types"
)

func TestReplConfig_NilDeref(t *testing.T) {
	replicationConfig := &b2types.ReplicationConfiguration{}
	bucket := &b2Bucket{b: &base.Bucket{
		ReplicationConfiguration: replicationConfig,
	}}

	panicValue, stack := recoverPanic(func() {
		_ = bucket.updateBucket(context.Background(), &BucketAttrs{Info: map[string]string{"key": "value"}})
	})
	if panicValue == nil {
		t.Fatal("expected nil-client panic from base.Bucket.Update")
	}
	if !strings.Contains(string(stack), "base.(*Bucket).Update") {
		t.Fatal("ReplicationConfig guard panicked before base.Bucket.Update")
	}
	if bucket.b.ReplicationConfiguration != replicationConfig {
		t.Fatal("omitting ReplicationConfig changed the cached configuration")
	}
}

func TestReplConfig_SilentDrop(t *testing.T) {
	bucket := &b2Bucket{b: &base.Bucket{}}
	attrs := &BucketAttrs{
		ReplicationConfig: &ReplicationConfiguration{
			AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "source-key"},
		},
	}

	panicValue, stack := recoverPanic(func() {
		_ = bucket.updateBucket(context.Background(), attrs)
	})
	if panicValue == nil {
		t.Fatal("expected nil-client panic from base.Bucket.Update")
	}
	if !strings.Contains(string(stack), "base.(*Bucket).Update") {
		t.Fatal("ReplicationConfig guard panicked before base.Bucket.Update")
	}
	if bucket.b.ReplicationConfiguration == nil {
		t.Fatal("ReplicationConfig was not copied into the base bucket")
	}
	if bucket.b.ReplicationConfiguration.AsReplicationSource.KeyID != "source-key" {
		t.Fatal("ReplicationConfig source key was not copied")
	}
}

func recoverPanic(fn func()) (panicValue any, stack []byte) {
	defer func() {
		panicValue = recover()
		if panicValue != nil {
			stack = debug.Stack()
		}
	}()
	fn()
	return nil, nil
}
