package base

import (
	"context"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Call base directly: the b2 converter normalizes nil before reaching this layer.
// https://www.backblaze.com/apidocs/b2-set-bucket-notification-rules
func TestSetNotificationRulesClearsNilAndEmpty(t *testing.T) {
	for _, test := range []struct {
		name  string
		rules []b2types.EventNotificationRule
	}{
		{name: "nil"},
		{name: "empty", rules: []b2types.EventNotificationRule{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.Reply("b2_authorize_account", http.StatusOK, v4AuthJSON(srv.URL(), nil, nil, ""))
			srv.StrictReplySequence("b2_set_bucket_notification_rules", b2wiretest.Response{
				Body: `{"bucketId":"bucket-id","eventNotificationRules":[]}`,
			})
			ctx := context.Background()
			account, err := AuthorizeAccount(ctx, "account-id", "key", SetAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			bucket := &Bucket{ID: "bucket-id", b2: account}
			if err := bucket.SetNotificationRules(ctx, test.rules); err != nil {
				t.Fatal(err)
			}
			request := srv.Last(t, "b2_set_bucket_notification_rules")
			if request.Method != http.MethodPost {
				t.Errorf("set method = %s, want POST", request.Method)
			}
			b2wiretest.AssertJSON(t, request, "bucketId", "bucket-id")
			b2wiretest.AssertJSON(t, request, "eventNotificationRules", []any{})
		})
	}
}
