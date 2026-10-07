package b2

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The cached bucket must not hold an empty default encryption after an update,
// so a second update on the same handle (no Attrs refresh in between) does not
// re-send it. The stub rejects an empty mode as B2 does.
func TestRepeatBucketUpdateDoesNotResendEmptyDefaultSSE(t *testing.T) {
	noDefaultSSE := map[string]interface{}{
		"isClientAuthorizedToRead": true,
		"value":                    map[string]interface{}{"mode": nil, "algorithm": nil},
	}
	bucketJSON := map[string]interface{}{
		"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate",
		"bucketInfo": map[string]string{}, "revision": 1,
		"defaultServerSideEncryption": noDefaultSSE,
		"fileLockConfiguration":       map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
		"replicationConfiguration":    map[string]interface{}{"value": nil},
	}
	var updates []map[string]json.RawMessage
	transport := bucketInfoOrSSETransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			return jsonResponse(req, http.StatusOK, map[string]interface{}{
				"accountId": "account", "authorizationToken": "token",
				"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
					"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
				}},
			})
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			return jsonResponse(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{bucketJSON}})
		case strings.HasSuffix(req.URL.Path, "/b2_update_bucket"):
			var body map[string]json.RawMessage
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			updates = append(updates, body)
			if raw, ok := body["defaultServerSideEncryption"]; ok {
				return jsonResponse(req, http.StatusBadRequest, map[string]interface{}{
					"status": 400, "code": "bad_request",
					"message": "Unsupported bucket default server-side encryption mode: (sent " + string(raw) + ")",
				})
			}
			return jsonResponse(req, http.StatusOK, bucketJSON)
		}
		t.Errorf("unexpected request %s", req.URL)
		return jsonResponse(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
	})

	ctx := context.Background()
	client, err := NewClient(ctx, "account", "key", APIBase("https://b2.test"), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	for i, info := range []map[string]string{{"first": "1"}, {"second": "2"}} {
		if err := bucket.Update(ctx, &BucketAttrs{Info: info}); err != nil {
			t.Fatalf("update %d on the same handle: %v", i+1, err)
		}
	}
	if len(updates) != 2 {
		t.Fatalf("got %d update requests, want 2", len(updates))
	}
}

type bucketInfoOrSSETransport func(*http.Request) (*http.Response, error)

func (f bucketInfoOrSSETransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
