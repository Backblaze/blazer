package base

import (
	"context"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestLifecycleRawCreateAndUpdateResponses(t *testing.T) {
	ctx := context.Background()
	srv := b2wiretest.New(t)
	const body = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[{"fileNamePrefix":"logs/","daysFromHidingToDeleting":2,"daysFromStartingToCancelingUnfinishedLargeFiles":7,"futurePolicy":3}],"revision":1}`
	srv.Reply("b2_create_bucket", http.StatusOK, body)
	srv.Reply("b2_update_bucket", http.StatusOK, body)
	root, err := AuthorizeAccount(ctx, "a", "k", SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"fileNamePrefix": "logs/", "daysFromHidingToDeleting": float64(2), "daysFromStartingToCancelingUnfinishedLargeFiles": float64(7), "futurePolicy": float64(3)}}
	for i := 0; i < 2; i++ {
		bucket, err = bucket.Update(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b2wiretest.AssertJSON(t, srv.Last(t, "b2_update_bucket"), "lifecycleRules", want)
	}
}
