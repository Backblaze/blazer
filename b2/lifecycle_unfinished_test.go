package b2

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestUnfinishedLifecycleAttrsUpdateRoundTrip(t *testing.T) {
	const field = "daysFromStartingToCancelingUnfinishedLargeFiles"
	f := newLCFixture(t, map[string]interface{}{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": 1, field: 7})
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs.LifecycleRules) != 1 || attrs.LifecycleRules[0].DaysUnfinishedUntilCanceled != 7 {
		t.Fatalf("Attrs lifecycle rules = %+v, want cancellation after 7 days", attrs.LifecycleRules)
	}
	attrs.Info = map[string]string{"owner": "test"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatal(err)
	}
	raw, ok := f.sent(0)
	if !ok {
		t.Fatal("update omitted lifecycleRules")
	}
	var rules []map[string]interface{}
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0][field] != float64(7) {
		t.Fatalf("update lifecycleRules = %s; want %s=7", raw, field)
	}
}

func TestLifecycleUnknownFieldsRoundTrip(t *testing.T) {
	want := map[string]interface{}{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": float64(2), "daysFromStartingToCancelingUnfinishedLargeFiles": float64(7), "futurePolicy": float64(3)}
	f := newLCFixture(t, want)
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attrs.Info = map[string]string{"owner": "test"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatal(err)
	}
	raw, ok := f.sent(0)
	if !ok {
		t.Fatal("missing lifecycleRules")
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("rules = %s, want complete rule %v", raw, want)
	}
}

func TestLifecycleUpdateResponseCancellationDays(t *testing.T) {
	f := newLCFixture(t, map[string]interface{}{"fileNamePrefix": "logs/", "daysFromStartingToCancelingUnfinishedLargeFiles": float64(7)})
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"owner": "test"}}); err != nil {
		t.Fatal(err)
	}
	cached := bucket.b.attrs()
	if len(cached.LifecycleRules) != 1 || cached.LifecycleRules[0].DaysUnfinishedUntilCanceled != 7 {
		t.Fatalf("update response rules = %+v, want cancellation 7", cached.LifecycleRules)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs.LifecycleRules) != 1 || attrs.LifecycleRules[0].DaysUnfinishedUntilCanceled != 7 {
		t.Fatalf("updated Attrs lifecycle = %+v, want cancellation 7", attrs.LifecycleRules)
	}
}

func TestLifecycleChangedRulesReplaceUnknownFields(t *testing.T) {
	f := newLCFixture(t, map[string]interface{}{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": float64(2), "futurePolicy": float64(3)})
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attrs.LifecycleRules[0].DaysHiddenUntilDeleted = 5
	if err = bucket.Update(ctx, attrs); err != nil {
		t.Fatal(err)
	}
	raw, ok := f.sent(0)
	if !ok {
		t.Fatal("missing lifecycleRules")
	}
	var got []map[string]interface{}
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": float64(5)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed rules = %s, want %v", raw, want)
	}
}
