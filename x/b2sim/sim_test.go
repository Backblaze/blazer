package b2sim

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
)

func writeObject(ctx context.Context, bucket *b2.Bucket, name string, data []byte, chunkSize int) (*b2.Object, error) {
	object := bucket.Object(name)
	w := object.NewWriter(ctx)
	if chunkSize != 0 {
		w.ChunkSize = chunkSize
		w.ConcurrentUploads = 3
	}
	if n, err := w.Write(data); err != nil || n != len(data) {
		return nil, fmt.Errorf("write %s: n=%d err=%v", name, n, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", name, err)
	}
	return object, nil
}

func readObject(ctx context.Context, object *b2.Object, offset, length int64) ([]byte, error) {
	r := object.NewRangeReader(ctx, offset, length)
	data, readErr := io.ReadAll(r)
	closeErr := r.Close()
	if readErr != nil {
		return nil, readErr
	}
	return data, closeErr
}

func TestGoldenPath(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "golden", &b2.BucketAttrs{
		Type: b2.Private, Info: map[string]string{"purpose": "offline"},
		FileLockEnabled: true, DefaultServerSideEncryption: b2.SSEB2WithAES256(),
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Info["purpose"] != "offline" || !attrs.FileLockEnabled || attrs.DefaultServerSideEncryption == nil || attrs.DefaultServerSideEncryption.Mode != "SSE-B2" {
		t.Fatalf("bucket attributes did not round-trip: %+v", attrs)
	}
	listedBuckets, err := client.ListBuckets(ctx)
	if err != nil || len(listedBuckets) != 1 || listedBuckets[0].ID() != bucket.ID() {
		t.Fatalf("list buckets: count=%d err=%v", len(listedBuckets), err)
	}
	small := []byte("simulated simple upload")
	smallObject, err := writeObject(ctx, bucket, "small.txt", small, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, smallObject, 0, -1)
	if err != nil || !bytes.Equal(got, small) {
		t.Fatalf("simple download: bytes=%q err=%v", got, err)
	}
	got, err = readObject(ctx, smallObject, 5, 6)
	if err != nil || !bytes.Equal(got, small[5:11]) {
		t.Fatalf("range download: bytes=%q err=%v", got, err)
	}
	large := bytes.Repeat([]byte("multipart!"), 1_100_001) // over two 5 MB parts
	largeObject, err := writeObject(ctx, bucket, "large.txt", large, 5_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if calls := sim.Calls(); calls["b2_start_large_file"] != 1 || calls["b2_upload_part"] < 2 || calls["b2_finish_large_file"] != 1 {
		t.Fatalf("large upload did not use multipart wire path: %v", calls)
	}
	got, err = readObject(ctx, largeObject, 0, -1)
	if err != nil || !bytes.Equal(got, large) {
		t.Fatalf("multipart download: bytes=%d err=%v", len(got), err)
	}
	iter := bucket.List(ctx)
	var names []string
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"large.txt", "small.txt"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed names = %q, want %q", names, want)
	}
	for _, object := range []*b2.Object{smallObject, largeObject} {
		if err := object.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if got := sim.Buckets(); len(got) != 0 {
		t.Fatalf("buckets after delete = %q", got)
	}
}

func TestSeedAndConcurrentUploads(t *testing.T) {
	ctx := context.Background()
	sim := New()
	id := sim.Seed("seeded")
	if sim.Seed("seeded") != id || !reflect.DeepEqual(sim.Buckets(), []string{"seeded"}) {
		t.Fatal("seed was not idempotent")
	}
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "seeded")
	if err != nil || bucket.ID() != id {
		t.Fatalf("seeded bucket: %v", err)
	}
	const count = 8
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent-%02d.txt", i)
			payload := []byte(name)
			object, err := writeObject(ctx, bucket, name, payload, 0)
			if err == nil {
				var got []byte
				got, err = readObject(ctx, object, 0, -1)
				if err == nil && !bytes.Equal(got, payload) {
					err = fmt.Errorf("%s: download mismatch", name)
				}
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	iter := bucket.List(ctx)
	var objects []*b2.Object
	for iter.Next() {
		objects = append(objects, iter.Object())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if len(objects) != count {
		t.Fatalf("listed %d objects, want %d", len(objects), count)
	}
	for _, object := range objects {
		if err := object.Delete(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEscapedNameAndVersionPagination(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "versions", nil)
	if err != nil {
		t.Fatal(err)
	}
	const name = "folder/100% space+name.txt"
	first, err := writeObject(ctx, bucket, name, []byte("first"), 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeObject(ctx, bucket, name, []byte("second"), 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readObject(ctx, second, 0, -1)
	if err != nil || string(got) != "second" {
		t.Fatalf("escaped-name download = %q, %v", got, err)
	}
	iter := bucket.List(ctx, b2.ListHidden(), b2.ListPageSize(1))
	var ids []string
	for iter.Next() {
		ids = append(ids, iter.Object().ID())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{second.ID(), first.ID()}) {
		t.Fatalf("paginated versions = %q, want newest then oldest", ids)
	}
	if err := first.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bucket.Delete(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	sim := New()
	client, err := b2.NewClient(ctx, "id", "key", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "settings", &b2.BucketAttrs{
		Type: b2.Private, FileLockEnabled: true,
		DefaultServerSideEncryption: b2.SSEB2WithAES256(),
		LifecycleRules:              []b2.LifecycleRule{{Prefix: "old/", DaysNewUntilHidden: 2}},
		CORSRules:                   []b2.CORSRule{{Name: "test", AllowedOrigins: []string{"https://example.test"}, AllowedOperations: []string{"b2_download_file_by_name"}, MaxAgeSeconds: 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !attrs.FileLockEnabled || attrs.DefaultServerSideEncryption == nil || attrs.DefaultServerSideEncryption.Algorithm != "AES256" {
		t.Fatalf("file lock/SSE not returned: %+v", attrs)
	}
	if len(attrs.LifecycleRules) != 1 || attrs.LifecycleRules[0].Prefix != "old/" || len(attrs.CORSRules) != 1 || attrs.CORSRules[0].Name != "test" {
		t.Fatalf("lifecycle/CORS not returned: %+v", attrs)
	}
	config := &b2.ReplicationConfiguration{AsReplicationSource: b2.AsReplicationSource{SourceApplicationKeyID: "key-id", ReplicationRules: []b2.ReplicationRules{{ReplicationRuleName: "rule", DestinationBucketID: "dest-id", Priority: 1, IsEnabled: true}}}}
	if err := bucket.Update(ctx, &b2.BucketAttrs{ReplicationConfig: config, DefaultRetention: &b2.Retention{Mode: "governance", Period: &b2.RetentionPeriod{Duration: 1, Unit: "days"}}}); err != nil {
		t.Fatal(err)
	}
	attrs, err = bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.ReplicationConfig == nil || attrs.ReplicationConfig.AsReplicationSource.SourceApplicationKeyID != "key-id" || len(attrs.ReplicationConfig.AsReplicationSource.ReplicationRules) != 1 {
		t.Fatalf("replication not returned: %+v", attrs.ReplicationConfig)
	}
	if attrs.DefaultRetention == nil || attrs.DefaultRetention.Mode != "governance" || attrs.DefaultRetention.Period == nil || attrs.DefaultRetention.Period.Duration != 1 {
		t.Fatalf("retention not returned: %+v", attrs.DefaultRetention)
	}
	object := bucket.Object("locked.txt")
	until := time.Unix(2_000_000_000, 0)
	w := object.NewWriter(ctx, b2.WithFileRetention(&b2.FileRetention{Mode: b2.RetentionGovernance, RetainUntil: until}), b2.WithLegalHold(b2.LegalHoldOn))
	if _, err := w.Write([]byte("lock data")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fileAttrs, err := object.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fileAttrs.Retention == nil || fileAttrs.Retention.Mode != b2.RetentionGovernance || !fileAttrs.Retention.RetainUntil.Equal(until) || fileAttrs.LegalHold != b2.LegalHoldOn {
		t.Fatalf("file lock metadata not returned: %+v", fileAttrs)
	}
}
