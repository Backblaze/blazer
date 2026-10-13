package b2

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func notificationJSON(req *http.Request, status int, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
		Request:    req,
	}, nil
}

// notificationFixture acts like the two notification-rules endpoints: every
// set replaces the complete rule list, and a get returns that latest list.
type notificationFixture struct {
	t      *testing.T
	rules  []map[string]interface{}
	sets   []map[string]json.RawMessage
	client *Client
}

func newNotificationFixture(t *testing.T) *notificationFixture {
	t.Helper()
	f := &notificationFixture{t: t}
	bucketJSON := func() map[string]interface{} {
		return map[string]interface{}{
			"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
			"bucketInfo": map[string]string{}, "revision": 1, "lifecycleRules": []interface{}{},
			"fileLockConfiguration":    map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
			"replicationConfiguration": map[string]interface{}{"value": nil},
		}
	}
	transport := notificationTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			return notificationJSON(req, http.StatusOK, map[string]interface{}{
				"accountId": "account", "authorizationToken": "token",
				"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test"}},
			})
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			return notificationJSON(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{bucketJSON()}})
		case strings.HasSuffix(req.URL.Path, "/b2_get_bucket_notification_rules"):
			if req.Method != http.MethodGet || req.URL.Query().Get("bucketId") != "bucket-id" {
				t.Errorf("get request = %s %s, want GET with bucketId", req.Method, req.URL)
			}
			return notificationJSON(req, http.StatusOK, map[string]interface{}{
				"bucketId": "bucket-id", "eventNotificationRules": f.rules,
			})
		case strings.HasSuffix(req.URL.Path, "/b2_set_bucket_notification_rules"):
			if req.Method != http.MethodPost {
				t.Errorf("set request method = %s, want POST", req.Method)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			rawRules, ok := body["eventNotificationRules"]
			if !ok {
				t.Errorf("set request omitted eventNotificationRules")
				return notificationJSON(req, http.StatusBadRequest, map[string]interface{}{"status": 400, "code": "bad_request", "message": "missing rules"})
			}
			var rules []map[string]interface{}
			if err := json.Unmarshal(rawRules, &rules); err != nil {
				return nil, err
			}
			f.sets = append(f.sets, body)
			f.rules = rules
			return notificationJSON(req, http.StatusOK, map[string]interface{}{
				"bucketId": "bucket-id", "eventNotificationRules": rules,
			})
		}
		t.Errorf("unexpected request %s", req.URL)
		return notificationJSON(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
	})
	client, err := NewClient(context.Background(), "account", "key", APIBase("https://b2.test"), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	f.client = client
	return f
}

func TestNotificationRulesGetAndReplace(t *testing.T) {
	ctx := context.Background()
	f := newNotificationFixture(t)
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}

	empty, err := bucket.NotificationRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("initial NotificationRules() = %#v, want none", empty)
	}

	want := []NotificationRule{{
		EventTypes:        []string{"b2:ObjectCreated:Upload"},
		IsEnabled:         true,
		MaxEventsPerBatch: 2,
		Name:              "uploads",
		ObjectNamePrefix:  "st/",
		TargetConfiguration: NotificationTargetConfiguration{
			TargetType: NotificationTargetWebhook,
			Webhook: &WebhookNotificationTarget{
				URL: "https://example.com/notify",
				CustomHeaders: []NotificationCustomHeader{{
					Name:  "X-Notification-Test",
					Value: "enabled",
				}},
			},
		},
	}}
	if err := bucket.SetNotificationRules(ctx, want); err != nil {
		t.Fatal(err)
	}
	if len(f.sets) != 1 {
		t.Fatalf("set calls = %d, want 1", len(f.sets))
	}
	assertWebhookTargetEncoding(t, f.sets[0])

	got, err := bucket.NotificationRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NotificationRules() = %#v, want %#v", got, want)
	}

	// b2_set_bucket_notification_rules replaces the whole list. A second set
	// with a different singleton must therefore leave only that new rule.
	replacement := []NotificationRule{{
		EventTypes:          []string{"b2:ObjectDeleted:*"},
		IsEnabled:           true,
		Name:                "deletes",
		ObjectNamePrefix:    "archive/",
		TargetConfiguration: NotificationTargetConfiguration{TargetType: NotificationTargetWebhook, Webhook: &WebhookNotificationTarget{URL: "https://example.com/deletes"}},
	}}
	if err := bucket.SetNotificationRules(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	got, err = bucket.NotificationRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, replacement) {
		t.Errorf("NotificationRules() after replacement = %#v, want %#v", got, replacement)
	}

	if err := bucket.SetNotificationRules(ctx, []NotificationRule{}); err != nil {
		t.Fatal(err)
	}
	if got := string(f.sets[len(f.sets)-1]["eventNotificationRules"]); got != "[]" {
		t.Errorf("clear request eventNotificationRules = %s, want []", got)
	}
	got, err = bucket.NotificationRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("NotificationRules() after clear = %#v, want none", got)
	}
}

func assertWebhookTargetEncoding(t *testing.T, request map[string]json.RawMessage) {
	t.Helper()
	var rules []map[string]interface{}
	if err := json.Unmarshal(request["eventNotificationRules"], &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("encoded rules = %#v, want one rule", rules)
	}
	target, ok := rules[0]["targetConfiguration"].(map[string]interface{})
	if !ok {
		t.Fatalf("targetConfiguration = %#v, want object", rules[0]["targetConfiguration"])
	}
	if target["targetType"] != "webhook" || target["url"] != "https://example.com/notify" {
		t.Errorf("targetConfiguration = %#v, want webhook target with its URL", target)
	}
	if _, nested := target["webhook"]; nested {
		t.Errorf("targetConfiguration = %#v, want a tagged union flattened to B2's wire schema", target)
	}
	headers, ok := target["customHeaders"].([]interface{})
	if !ok || len(headers) != 1 {
		t.Errorf("customHeaders = %#v, want one header", target["customHeaders"])
	}
}
