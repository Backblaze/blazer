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

type lcTransport func(*http.Request) (*http.Response, error)

func (f lcTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func lcJSON(req *http.Request, status int, body interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b)), Request: req}, nil
}

// lcFixture is a stateful stub of one bucket's lifecycle rules. A b2_update_bucket
// that carries lifecycleRules replaces them; one that omits the field leaves
// them alone, as B2 documents.
type lcFixture struct {
	t           *testing.T
	failUpdates bool
	rules       []map[string]interface{}
	updates     []map[string]json.RawMessage
	client      *Client
}

func newLCFixture(t *testing.T, rules ...map[string]interface{}) *lcFixture {
	t.Helper()
	f := &lcFixture{t: t, rules: rules}
	bucketJSON := func() map[string]interface{} {
		rs := f.rules
		if rs == nil {
			rs = []map[string]interface{}{}
		}
		return map[string]interface{}{
			"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
			"bucketInfo": map[string]string{}, "revision": 1, "lifecycleRules": rs,
			"fileLockConfiguration":    map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
			"replicationConfiguration": map[string]interface{}{"value": nil},
		}
	}
	transport := lcTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			return lcJSON(req, http.StatusOK, map[string]interface{}{
				"accountId": "account", "authorizationToken": "token",
				"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test"}},
			})
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			return lcJSON(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{bucketJSON()}})
		case strings.HasSuffix(req.URL.Path, "/b2_update_bucket"):
			var body map[string]json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			f.updates = append(f.updates, body)
			if f.failUpdates {
				return lcJSON(req, http.StatusBadRequest, map[string]interface{}{"status": 400, "code": "bad_request", "message": "rejected"})
			}
			if raw, ok := body["lifecycleRules"]; ok {
				var rs []map[string]interface{}
				if err := json.Unmarshal(raw, &rs); err != nil {
					return lcJSON(req, http.StatusBadRequest, map[string]interface{}{"status": 400, "code": "bad_request", "message": "lifecycleRules must be an array: " + string(raw)})
				}
				f.rules = rs
			}
			return lcJSON(req, http.StatusOK, bucketJSON())
		}
		t.Errorf("unexpected request %s", req.URL)
		return lcJSON(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
	})
	c, err := NewClient(context.Background(), "account", "key", APIBase("https://b2.test"), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	f.client = c
	return f
}

func lcRule(prefix string, days int) map[string]interface{} {
	return map[string]interface{}{"fileNamePrefix": prefix, "daysFromHidingToDeleting": days}
}

func (f *lcFixture) sent(i int) (json.RawMessage, bool) {
	f.t.Helper()
	if len(f.updates) <= i {
		f.t.Fatalf("only %d b2_update_bucket request(s) were sent", len(f.updates))
	}
	raw, ok := f.updates[i]["lifecycleRules"]
	return raw, ok
}

// nil leaves lifecycle rules alone: the request must not carry the field, with
// or without rules on the bucket.
func TestUpdateWithNilLifecycleRulesLeavesThemAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []map[string]interface{}
	}{
		{"a bucket with rules", []map[string]interface{}{lcRule("logs/", 7)}},
		{"a bucket with no rules", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLCFixture(t, tc.rules...)
			ctx := context.Background()
			bucket, err := f.client.Bucket(ctx, "bucket")
			if err != nil {
				t.Fatal(err)
			}
			if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"owner": "me"}}); err != nil {
				t.Fatal(err)
			}
			if raw, ok := f.sent(0); ok {
				t.Errorf("update carried lifecycleRules %s although the caller did not set them", raw)
			}
		})
	}
}

// Rules added elsewhere (the S3 API, the web UI) after this handle was cached
// must survive an unrelated update.
func TestUnrelatedUpdateDoesNotWipeRulesAddedElsewhere(t *testing.T) {
	f := newLCFixture(t) // no rules when the handle is cached
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	f.rules = []map[string]interface{}{lcRule("added-by-s3/", 30)} // changed behind our back

	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"owner": "me"}}); err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 1 {
		t.Errorf("server lifecycle rules after an unrelated update = %v, want the rule added elsewhere to survive", f.rules)
	}
}

// An empty, non-nil slice is the documented way to remove every rule.
func TestUpdateWithEmptyLifecycleRulesClearsThem(t *testing.T) {
	f := newLCFixture(t, lcRule("logs/", 7))
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{LifecycleRules: []LifecycleRule{}}); err != nil {
		t.Fatal(err)
	}
	raw, ok := f.sent(0)
	if !ok || strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("lifecycleRules in the request = %s (present %v), want []", raw, ok)
	}
	if len(f.rules) != 0 {
		t.Errorf("server rules after clearing = %v, want none", f.rules)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs.LifecycleRules) != 0 {
		t.Errorf("Attrs().LifecycleRules after clearing = %v, want none", attrs.LifecycleRules)
	}
}

func TestUpdateWithLifecycleRulesReplacesThem(t *testing.T) {
	f := newLCFixture(t, lcRule("logs/", 7))
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	want := []LifecycleRule{{Prefix: "tmp/", DaysHiddenUntilDeleted: 1}}
	if err := bucket.Update(ctx, &BucketAttrs{LifecycleRules: want}); err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attrs.LifecycleRules, want) {
		t.Errorf("Attrs().LifecycleRules = %+v, want %+v", attrs.LifecycleRules, want)
	}
}

// A failed update must leave the cached rules as they were.
func TestFailedUpdateRestoresCachedLifecycleRules(t *testing.T) {
	f := newLCFixture(t, lcRule("logs/", 7))
	ctx := context.Background()
	bucket, err := f.client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	cached := func() int {
		return len(bucket.b.(*beBucket).b2bucket.(*b2Bucket).b.LifecycleRules)
	}
	if cached() != 1 {
		t.Fatalf("setup: %d cached rules, want 1", cached())
	}
	f.failUpdates = true
	if err := bucket.Update(ctx, &BucketAttrs{LifecycleRules: []LifecycleRule{}}); err == nil {
		t.Fatal("Update succeeded, want the rejected request to fail")
	}
	if got := cached(); got != 1 {
		t.Errorf("cached rules after a failed update = %d, want the previous 1", got)
	}
	// An unrelated update that fails must not lose them either.
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"k": "v"}}); err == nil {
		t.Fatal("Update succeeded, want the rejected request to fail")
	}
	if got := cached(); got != 1 {
		t.Errorf("cached rules after a failed unrelated update = %d, want the previous 1", got)
	}
}
