// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

type emptyDefaultSSETransport struct {
	t           *testing.T
	bucket      map[string]interface{}
	updateCalls int
}

func (t *emptyDefaultSSETransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.t.Helper()

	var body map[string]interface{}
	if req.Body != nil {
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.t.Fatalf("decode %s request: %v", req.Header.Get("X-Blazer-Method"), err)
		}
	}

	switch req.Header.Get("X-Blazer-Method") {
	case "b2_authorize_account":
		return jsonResponse(req, http.StatusOK, map[string]interface{}{
			"accountId":          "account-id",
			"authorizationToken": "auth-token",
			"apiInfo": map[string]interface{}{
				"storageApi": map[string]interface{}{
					"apiUrl":                  "https://api.example.com",
					"downloadUrl":             "https://download.example.com",
					"s3ApiUrl":                "https://s3.example.com",
					"absoluteMinimumPartSize": 5000000,
				},
			},
		})
	case "b2_list_buckets":
		buckets := []interface{}{}
		if t.bucket != nil {
			buckets = append(buckets, t.bucket)
		}
		return jsonResponse(req, http.StatusOK, map[string]interface{}{"buckets": buckets})
	case "b2_create_bucket":
		t.bucket = bucketWithEmptyDefaultSSE(body)
		return jsonResponse(req, http.StatusOK, t.bucket)
	case "b2_update_bucket":
		if _, ok := body["defaultServerSideEncryption"]; ok {
			return jsonResponse(req, http.StatusBadRequest, map[string]interface{}{
				"status":  http.StatusBadRequest,
				"code":    "bad_request",
				"message": "Unsupported bucket default server-side encryption mode:",
			})
		}
		t.updateCalls++
		t.bucket["bucketInfo"] = body["bucketInfo"]
		t.bucket["revision"] = 2
		return jsonResponse(req, http.StatusOK, t.bucket)
	default:
		t.t.Fatalf("unexpected B2 method %q", req.Header.Get("X-Blazer-Method"))
		return nil, nil
	}
}

func bucketWithEmptyDefaultSSE(createRequest map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"bucketId":       "bucket-id",
		"bucketName":     createRequest["bucketName"],
		"bucketType":     createRequest["bucketType"],
		"bucketInfo":     createRequest["bucketInfo"],
		"lifecycleRules": createRequest["lifecycleRules"],
		"revision":       1,
		"defaultServerSideEncryption": map[string]interface{}{
			"isClientAuthorizedToRead": true,
			"value": map[string]interface{}{
				"mode":      nil,
				"algorithm": nil,
			},
		},
		"fileLockConfiguration": map[string]interface{}{
			"isClientAuthorizedToRead": true,
			"value": map[string]interface{}{
				"defaultRetention": map[string]interface{}{
					"mode": nil,
				},
				"isFileLockEnabled": false,
			},
		},
		"replicationConfiguration": map[string]interface{}{
			"isClientAuthorizedToRead": true,
			"value":                    nil,
		},
	}
}

func jsonResponse(req *http.Request, status int, body interface{}) (*http.Response, error) {
	contents, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(contents)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestBucketEmptyDefaultSSERoundTrip(t *testing.T) {
	ctx := context.Background()
	transport := &emptyDefaultSSETransport{t: t}
	client, err := NewClient(ctx, "account-id", "application-key", APIBase("https://api.example.com"), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}

	bucket, err := client.NewBucket(ctx, "bucket-name", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	if got := bucket.b.attrs().DefaultServerSideEncryption; got != nil {
		t.Errorf("created bucket default SSE = %#v; want nil", got)
	}

	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs.DefaultServerSideEncryption; got != nil {
		t.Errorf("read-back bucket default SSE = %#v; want nil", got)
	}

	// Simulate a handle cached before normalization; Update must omit this too.
	bucket.b.(*beBucket).b2bucket.(*b2Bucket).b.DefaultServerSideEncryption = &b2types.ServerSideEncryption{}
	attrs.Info = map[string]string{"updated": "true"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatalf("update unrelated bucket info: %v", err)
	}
	if transport.updateCalls != 1 {
		t.Fatalf("update calls = %d; want 1", transport.updateCalls)
	}
	if got := bucket.b.attrs().DefaultServerSideEncryption; got != nil {
		t.Errorf("cached bucket default SSE after first update = %#v; want nil", got)
	}

	attrs, err = bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs.DefaultServerSideEncryption; got != nil {
		t.Errorf("updated bucket default SSE = %#v; want nil", got)
	}
	if got := attrs.Info["updated"]; got != "true" {
		t.Errorf("updated bucket info = %q; want true", got)
	}

	attrs.Info = map[string]string{"updated": "again"}
	if err := bucket.Update(ctx, attrs); err != nil {
		t.Fatalf("second update on same bucket handle: %v", err)
	}
	if transport.updateCalls != 2 {
		t.Fatalf("update calls = %d; want 2", transport.updateCalls)
	}
	attrs, err = bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := attrs.DefaultServerSideEncryption; got != nil {
		t.Errorf("bucket default SSE after second update = %#v; want nil", got)
	}
	if got := attrs.Info["updated"]; got != "again" {
		t.Errorf("bucket info after second update = %q; want again", got)
	}
}
