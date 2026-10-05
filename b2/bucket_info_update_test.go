package b2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type bucketInfoRoundTripper func(*http.Request) (*http.Response, error)

func (f bucketInfoRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBucketInfoUpdateNilEmptyAndSet(t *testing.T) {
	tests := []struct {
		name       string
		info       map[string]string
		bucketType BucketType
		want       map[string]string
		wantField  bool
	}{
		{name: "nil preserves during type update", bucketType: Public, want: map[string]string{"old": "value"}},
		{name: "empty clears", info: map[string]string{}, want: map[string]string{}, wantField: true},
		{name: "nonempty sets", info: map[string]string{"new": "value"}, want: map[string]string{"new": "value"}, wantField: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stored := map[string]string{"old": "value"}
			storedType := string(Private)
			var updateBody map[string]json.RawMessage
			bucketResponse := func() map[string]interface{} {
				return map[string]interface{}{
					"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": storedType,
					"bucketInfo": stored, "revision": 1,
					"fileLockConfiguration":    map[string]interface{}{"value": map[string]interface{}{"isFileLockEnabled": false}},
					"replicationConfiguration": map[string]interface{}{"value": nil},
				}
			}
			transport := bucketInfoRoundTripper(func(req *http.Request) (*http.Response, error) {
				var response interface{}
				switch {
				case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
					response = map[string]interface{}{
						"accountId": "account", "authorizationToken": "token",
						"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
							"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
						}},
					}
				case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
					response = map[string]interface{}{"buckets": []interface{}{bucketResponse()}}
				case strings.HasSuffix(req.URL.Path, "/b2_update_bucket"):
					if err := json.NewDecoder(req.Body).Decode(&updateBody); err != nil {
						return nil, err
					}
					if raw, ok := updateBody["bucketType"]; ok {
						if err := json.Unmarshal(raw, &storedType); err != nil {
							return nil, err
						}
					}
					if raw, ok := updateBody["bucketInfo"]; ok {
						stored = nil
						if err := json.Unmarshal(raw, &stored); err != nil {
							return nil, err
						}
					}
					response = bucketResponse()
				default:
					return nil, fmt.Errorf("unexpected request %s", req.URL)
				}
				body, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
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
			if err := bucket.Update(ctx, &BucketAttrs{Type: tc.bucketType, Info: tc.info}); err != nil {
				t.Fatal(err)
			}
			attrs, err := bucket.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(attrs.Info, tc.want) {
				t.Errorf("bucket Info = %v, want %v", attrs.Info, tc.want)
			}
			if tc.bucketType != UnknownType && attrs.Type != tc.bucketType {
				t.Errorf("bucket Type = %q, want %q", attrs.Type, tc.bucketType)
			}
			raw, present := updateBody["bucketInfo"]
			if present != tc.wantField {
				t.Fatalf("bucketInfo field present = %t, want %t; request = %s", present, tc.wantField, updateBody)
			}
			if present {
				var sent map[string]string
				if err := json.Unmarshal(raw, &sent); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(sent, tc.want) {
					t.Errorf("sent bucketInfo = %v, want %v", sent, tc.want)
				}
			}
		})
	}
}
