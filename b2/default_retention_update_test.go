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

	"github.com/Backblaze/blazer/internal/b2types"
)

type drTransport func(*http.Request) (*http.Response, error)

func (f drTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func drJSON(req *http.Request, status int, body interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b)), Request: req}, nil
}

// drFixture serves an Object Lock bucket whose default retention is 1 day of
// governance. A request that carries defaultRetention is refused with a 401
// unless allowRetentionWrite is set, as B2 does for a key that lacks
// writeBucketRetentions. Every b2_update_bucket body is recorded.
type drFixture struct {
	t                   *testing.T
	allowRetentionWrite bool
	updates             []map[string]json.RawMessage
}

func (f *drFixture) bucketJSON(duration int, unit string) map[string]interface{} {
	return map[string]interface{}{
		"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
		"bucketInfo": map[string]string{}, "revision": 1,
		"fileLockConfiguration": map[string]interface{}{
			"isClientAuthorizedToRead": true,
			"value": map[string]interface{}{
				"isFileLockEnabled": true,
				"defaultRetention": map[string]interface{}{
					"mode": "governance", "period": map[string]interface{}{"duration": duration, "unit": unit},
				},
			},
		},
		"replicationConfiguration": map[string]interface{}{"value": nil},
	}
}

func (f *drFixture) client() *Client {
	f.t.Helper()
	transport := drTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			return drJSON(req, http.StatusOK, map[string]interface{}{
				"accountId": "account", "authorizationToken": "token",
				"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
					"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
				}},
			})
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			return drJSON(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{f.bucketJSON(1, "days")}})
		case strings.HasSuffix(req.URL.Path, "/b2_update_bucket"):
			var body map[string]json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			f.updates = append(f.updates, body)
			if _, ok := body["defaultRetention"]; ok && !f.allowRetentionWrite {
				return drJSON(req, http.StatusUnauthorized, map[string]interface{}{
					"status": 401, "code": "unauthorized", "message": "requires writeBucketRetentions",
				})
			}
			return drJSON(req, http.StatusOK, f.bucketJSON(30, "days"))
		}
		f.t.Errorf("unexpected request %s", req.URL)
		return drJSON(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
	})
	c, err := NewClient(context.Background(), "account", "key", APIBase("https://b2.test"), Transport(transport))
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func TestUnrelatedUpdateDoesNotSendCachedDefaultRetention(t *testing.T) {
	f := &drFixture{t: t}
	ctx := context.Background()
	bucket, err := f.client().Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{Info: map[string]string{"owner": "me"}}); err != nil {
		t.Fatalf("an update that does not touch default retention failed: %v", err)
	}
	if _, sent := f.updates[0]["defaultRetention"]; sent {
		t.Errorf("update sent defaultRetention %s although the caller did not change it", f.updates[0]["defaultRetention"])
	}
}

// Attrs() now returns the default retention, so a read-modify-write hands the
// unchanged value back. That must not require the retention capability either.
func TestReadModifyWriteWithUnchangedRetentionDoesNotSendIt(t *testing.T) {
	f := &drFixture{t: t}
	ctx := context.Background()
	bucket, err := f.client().Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.DefaultRetention == nil || attrs.DefaultRetention.Mode != "governance" {
		t.Fatalf("Attrs().DefaultRetention = %+v, want governance", attrs.DefaultRetention)
	}
	attrs.Info = map[string]string{"owner": "me"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatalf("read-modify-write with an unchanged retention failed: %v", err)
	}
	if _, sent := f.updates[0]["defaultRetention"]; sent {
		t.Errorf("update sent the unchanged defaultRetention %s", f.updates[0]["defaultRetention"])
	}
}

func TestChangedDefaultRetentionIsSent(t *testing.T) {
	f := &drFixture{t: t, allowRetentionWrite: true}
	ctx := context.Background()
	bucket, err := f.client().Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	want := &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 30, Unit: "days"}}
	if err := bucket.Update(ctx, &BucketAttrs{DefaultRetention: want}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	raw, sent := f.updates[0]["defaultRetention"]
	if !sent {
		t.Fatal("a changed defaultRetention was not sent")
	}
	var got struct {
		Mode   string `json:"mode"`
		Period struct {
			Duration int    `json:"duration"`
			Unit     string `json:"unit"`
		} `json:"period"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != want.Mode || got.Period.Duration != want.Period.Duration || got.Period.Unit != want.Period.Unit {
		t.Errorf("sent defaultRetention = %+v, want %+v", got, want)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attrs.DefaultRetention, &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 1, Unit: "days"}}) {
		t.Logf("Attrs() after update reads the stubbed list response: %+v", attrs.DefaultRetention)
	}
}

// A failed update must leave the cached default retention as it was.
func TestFailedUpdateRestoresCachedDefaultRetention(t *testing.T) {
	f := &drFixture{t: t} // refuses any request that carries defaultRetention
	ctx := context.Background()
	bucket, err := f.client().Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	cached := func() *b2types.Retention {
		return bucket.b.(*beBucket).b2bucket.(*b2Bucket).b.DefaultRetention
	}
	want := &b2types.Retention{Mode: "governance", Period: &b2types.RetentionPeriod{Duration: 1, Unit: "days"}}
	if got := cached(); !reflect.DeepEqual(got, want) {
		t.Fatalf("setup: cached retention = %+v, want %+v", got, want)
	}

	changed := &Retention{Mode: "compliance", Period: &RetentionPeriod{Duration: 90, Unit: "days"}}
	if err := bucket.Update(ctx, &BucketAttrs{DefaultRetention: changed}); err == nil {
		t.Fatal("Update succeeded, want the refused request to fail")
	}
	if got := cached(); !reflect.DeepEqual(got, want) {
		t.Errorf("cached retention after a failed update = %+v, want the previous %+v", got, want)
	}
}

func TestUpdateWithRetentionWithoutPeriodDoesNotPanic(t *testing.T) {
	f := &drFixture{t: t, allowRetentionWrite: true}
	ctx := context.Background()
	bucket, err := f.client().Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	if err := bucket.Update(ctx, &BucketAttrs{DefaultRetention: &Retention{Mode: "compliance"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
