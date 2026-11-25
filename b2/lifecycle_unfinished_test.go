package b2

import (
	"context"
	"encoding/json"
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
