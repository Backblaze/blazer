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

			requests := srv.Calls("b2_set_bucket_notification_rules")
			if len(requests) != 1 {
				t.Fatalf("set requests = %d, want 1", len(requests))
			}
			request := requests[0]
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
	srv.Reply("b2_get_bucket_notification_rules", http.StatusOK, fmt.Sprintf(`{"bucketId":"bid","eventNotificationRules":[{"eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"isSuspended":true,"name":"uploads","objectNamePrefix":"","suspensionReason":"webhook returned 410","targetConfiguration":{"targetType":"webhook","url":"https://example.com/notify","hmacSha256SigningSecret":%q}}]}`, notificationRulesSigningSecret))
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

// TestNotificationRulesReadsWebhookWithoutSigningSecret asserts that an absent
// signing secret still decodes a webhook target. B2 omits the secret when one
// was not configured: https://www.backblaze.com/apidocs/b2-get-bucket-notification-rules
func TestNotificationRulesReadsWebhookWithoutSigningSecret(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_get_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[{"eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"name":"uploads","objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/notify"}}]}`)
	bucket := newNotificationRulesWireBucket(t, srv)

	rules, err := bucket.NotificationRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("NotificationRules() = %#v, want one rule", rules)
	}
	if rules[0].TargetConfiguration.Webhook == nil {
		t.Fatal("Webhook = nil, want webhook target")
	}
	if got := rules[0].TargetConfiguration.Webhook.HMACSHA256SigningSecret; got != "" {
		t.Errorf("Webhook.HMACSHA256SigningSecret = %q, want empty", got)
	}
}

// TestNotificationRulesRoundTripsCustomHeadersAndMaxEventsPerBatch asserts B2's
// documented webhook target configuration:
// https://www.backblaze.com/apidocs/b2-set-bucket-notification-rules
func TestNotificationRulesRoundTripsCustomHeadersAndMaxEventsPerBatch(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_set_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[]}`)
	srv.Reply("b2_get_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[{"eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"name":"uploads","objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/notify","customHeaders":[{"name":"X-Source","value":"blazer"},{"name":"X-Environment","value":"test"}],"maxEventsPerBatch":10}}]}`)
	bucket := newNotificationRulesWireBucket(t, srv)

	err := bucket.SetNotificationRules(context.Background(), []NotificationRule{{
		EventTypes:       []string{"b2:ObjectCreated:Upload"},
		IsEnabled:        true,
		Name:             "uploads",
		ObjectNamePrefix: "",
		TargetConfiguration: NotificationTargetConfiguration{
			TargetType: NotificationTargetWebhook,
			Webhook: &WebhookNotificationTarget{
				URL: "https://example.com/notify",
				CustomHeaders: []NotificationCustomHeader{
					{Name: "X-Source", Value: "blazer"},
					{Name: "X-Environment", Value: "test"},
				},
				MaxEventsPerBatch: 10,
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := srv.Last(t, "b2_set_bucket_notification_rules")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.customHeaders.0.name", "X-Source")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.customHeaders.0.value", "blazer")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.customHeaders.1.name", "X-Environment")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.customHeaders.1.value", "test")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.maxEventsPerBatch", 10)

	rules, err := bucket.NotificationRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].TargetConfiguration.Webhook == nil {
		t.Fatalf("NotificationRules() = %#v, want one webhook rule", rules)
	}
	webhook := rules[0].TargetConfiguration.Webhook
	if webhook.MaxEventsPerBatch != 10 {
		t.Errorf("Webhook.MaxEventsPerBatch = %d, want 10", webhook.MaxEventsPerBatch)
	}
	wantHeaders := []NotificationCustomHeader{{Name: "X-Source", Value: "blazer"}, {Name: "X-Environment", Value: "test"}}
	if len(webhook.CustomHeaders) != len(wantHeaders) {
		t.Fatalf("Webhook.CustomHeaders = %#v, want %#v", webhook.CustomHeaders, wantHeaders)
	}
	for i, want := range wantHeaders {
		if got := webhook.CustomHeaders[i]; got != want {
			t.Errorf("Webhook.CustomHeaders[%d] = %#v, want %#v", i, got, want)
		}
	}
}

// TestNotificationRulesKeepsEachRuleSigningSecret asserts that every rule's
// webhook configuration is encoded and decoded independently.
func TestNotificationRulesKeepsEachRuleSigningSecret(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_set_bucket_notification_rules", http.StatusOK, `{"bucketId":"bid","eventNotificationRules":[]}`)
	srv.Reply("b2_get_bucket_notification_rules", http.StatusOK, fmt.Sprintf(`{"bucketId":"bid","eventNotificationRules":[{"eventTypes":["b2:ObjectCreated:Upload"],"isEnabled":true,"name":"with-secret","objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/with-secret","hmacSha256SigningSecret":%q}},{"eventTypes":["b2:ObjectDeleted:*"],"isEnabled":true,"name":"without-secret","objectNamePrefix":"","targetConfiguration":{"targetType":"webhook","url":"https://example.com/without-secret"}}]}`, notificationRulesSigningSecret))
	bucket := newNotificationRulesWireBucket(t, srv)

	err := bucket.SetNotificationRules(context.Background(), []NotificationRule{
		{
			EventTypes:       []string{"b2:ObjectCreated:Upload"},
			IsEnabled:        true,
			Name:             "with-secret",
			ObjectNamePrefix: "",
			TargetConfiguration: NotificationTargetConfiguration{
				TargetType: NotificationTargetWebhook,
				Webhook: &WebhookNotificationTarget{
					URL:                     "https://example.com/with-secret",
					HMACSHA256SigningSecret: notificationRulesSigningSecret,
				},
			},
		},
		{
			EventTypes:       []string{"b2:ObjectDeleted:*"},
			IsEnabled:        true,
			Name:             "without-secret",
			ObjectNamePrefix: "",
			TargetConfiguration: NotificationTargetConfiguration{
				TargetType: NotificationTargetWebhook,
				Webhook:    &WebhookNotificationTarget{URL: "https://example.com/without-secret"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := srv.Last(t, "b2_set_bucket_notification_rules")
	b2wiretest.AssertJSON(t, request, "eventNotificationRules.0.targetConfiguration.hmacSha256SigningSecret", notificationRulesSigningSecret)
	b2wiretest.AssertNoJSON(t, request, "eventNotificationRules.1.targetConfiguration.hmacSha256SigningSecret")

	rules, err := bucket.NotificationRules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("NotificationRules() = %#v, want two rules", rules)
	}
	for i, want := range []string{notificationRulesSigningSecret, ""} {
		if rules[i].TargetConfiguration.Webhook == nil {
			t.Fatalf("rules[%d].Webhook = nil, want webhook target", i)
		}
		if got := rules[i].TargetConfiguration.Webhook.HMACSHA256SigningSecret; got != want {
			t.Errorf("rules[%d].Webhook.HMACSHA256SigningSecret = %q, want %q", i, got, want)
		}
	}
}
