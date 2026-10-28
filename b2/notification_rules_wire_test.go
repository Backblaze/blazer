package b2

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const notificationRulesSigningSecret = "abcdefghijklmnopqrstuvwxyz012345"

func newNotificationRulesWireBucket(t *testing.T, srv *b2wiretest.Server) *Bucket {
	t.Helper()
	srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(updateAuthJSON, srv.URL()))
	srv.Reply("b2_list_buckets", http.StatusOK, fmt.Sprintf(`{"buckets":[%s]}`, noReplicationBucketJSON))

	ctx := context.Background()
	client, err := NewClient(ctx, "a", "k", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

// TestSetNotificationRulesSendsSigningSecret asserts B2's documented set request:
// https://www.backblaze.com/apidocs/b2-set-bucket-notification-rules
func TestSetNotificationRulesSendsSigningSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{name: "with secret", secret: notificationRulesSigningSecret},
		{name: "without secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.Reply("b2_set_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[]}`)
			bucket := newNotificationRulesWireBucket(t, srv)

			err := bucket.SetNotificationRules(context.Background(), []NotificationRule{{
				EventTypes:       []string{"b2:ObjectCreated:Upload"},
				IsEnabled:        true,
				Name:             "uploads",
				ObjectNamePrefix: "",
				TargetConfiguration: NotificationTargetConfiguration{
					TargetType: NotificationTargetWebhook,
					Webhook: &WebhookNotificationTarget{
						URL:                     "https://example.com/notify",
						HMACSHA256SigningSecret: test.secret,
					},
				},
			}})
			if err != nil {
				t.Fatal(err)
			}

			request := srv.Last(t, "b2_set_bucket_notification_rules")
			b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.url", "https://example.com/notify")
			b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.targetType", "webhook")
			if test.secret == "" {
				b2wiretest.AssertNoJSON(t, request, "eventNotificationRules.0.targetConfiguration.hmacSha256SigningSecret")
			} else {
				b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.hmacSha256SigningSecret", notificationRulesSigningSecret)
			}
		})
	}
}

// TestNotificationRulesReadsSecretAndSuspension asserts B2's documented get response:
// https://www.backblaze.com/apidocs/b2-get-bucket-notification-rules
func TestNotificationRulesReadsSecretAndSuspension(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_get_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[{"eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"isSuspended":true,"name":"uploads","objectNamePrefix":"","suspensionReason":"webhook returned 410","targetConfiguration":{"targetType":"webhook","url":"https://example.com/notify","hmacSha256SigningSecret":"abcdefghijklmnopqrstuvwxyz012345"}}]}`)
	bucket := newNotificationRulesWireBucket(t, srv)

	rules, err := bucket.NotificationRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("NotificationRules() = %#v, want one rule", rules)
	}
	rule := rules[0]
	if !rule.IsSuspended {
		t.Errorf("IsSuspended = false, want true")
	}
	if rule.SuspensionReason != "webhook returned 410" {
		t.Errorf("SuspensionReason = %q, want %q", rule.SuspensionReason, "webhook returned 410")
	}
	if rule.Name != "uploads" {
		t.Errorf("Name = %q, want %q", rule.Name, "uploads")
	}
	if rule.TargetConfiguration.Webhook == nil {
		t.Fatal("Webhook = nil, want webhook target")
	}
	if rule.TargetConfiguration.Webhook.URL != "https://example.com/notify" {
		t.Errorf("Webhook.URL = %q, want %q", rule.TargetConfiguration.Webhook.URL, "https://example.com/notify")
	}
	if rule.TargetConfiguration.Webhook.HMACSHA256SigningSecret != notificationRulesSigningSecret {
		t.Errorf("Webhook.HMACSHA256SigningSecret = %q, want %q", rule.TargetConfiguration.Webhook.HMACSHA256SigningSecret, notificationRulesSigningSecret)
	}
}
