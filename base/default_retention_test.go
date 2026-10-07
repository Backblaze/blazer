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

package base

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

const defaultRetentionBucketResponse = `{
	"bucketId": "bucket-id",
	"bucketName": "bucket-name",
	"bucketType": "allPrivate",
	"bucketInfo": {},
	"lifecycleRules": [],
	"revision": 1,
	"fileLockConfiguration": {
		"isClientAuthorizedToRead": true,
		"value": {
			"defaultRetention": {
				"mode": "governance",
				"period": {"duration": 7, "unit": "days"}
			},
			"isFileLockEnabled": true
		}
	}
}`

type defaultRetentionResponseTransport struct {
	t *testing.T
}

func (t defaultRetentionResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.t.Helper()

	switch req.Header.Get("X-Blazer-Method") {
	case "b2_authorize_account":
		return defaultRetentionHTTPResponse(req, `{
			"accountId": "account-id",
			"authorizationToken": "authorization-token",
			"apiInfo": {
				"storageApi": {
					"apiUrl": "https://api.example.com",
					"downloadUrl": "https://download.example.com",
					"s3ApiUrl": "https://s3.example.com"
				}
			}
		}`), nil
	case "b2_create_bucket":
		return defaultRetentionHTTPResponse(req, defaultRetentionBucketResponse), nil
	case "b2_list_buckets":
		return defaultRetentionHTTPResponse(req, `{"buckets":[`+defaultRetentionBucketResponse+`]}`), nil
	default:
		t.t.Fatalf("unexpected B2 method %q", req.Header.Get("X-Blazer-Method"))
		return nil, nil
	}
}

func defaultRetentionHTTPResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestDefaultRetentionFromBucketResponses(t *testing.T) {
	ctx := context.Background()
	b2, err := AuthorizeAccount(ctx, "account-id", "application-key", Transport(defaultRetentionResponseTransport{t: t}))
	if err != nil {
		t.Fatal(err)
	}

	created, err := b2.CreateBucket(ctx, "bucket-name", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	checkDefaultRetention(t, "created bucket", created.DefaultRetention)

	buckets, err := b2.ListBuckets(ctx, "bucket-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 {
		t.Fatalf("listed buckets = %d; want 1", len(buckets))
	}
	checkDefaultRetention(t, "listed bucket", buckets[0].DefaultRetention)
}

func checkDefaultRetention(t *testing.T, label string, retention *b2types.Retention) {
	t.Helper()
	if retention == nil {
		t.Errorf("%s default retention = nil; want governance/7 days", label)
		return
	}
	if retention.Mode != "governance" {
		t.Errorf("%s default retention mode = %q; want governance", label, retention.Mode)
	}
	if retention.Period == nil {
		t.Errorf("%s default retention period = nil; want 7 days", label)
		return
	}
	if retention.Period.Duration != 7 || retention.Period.Unit != "days" {
		t.Errorf("%s default retention period = %d %s; want 7 days", label, retention.Period.Duration, retention.Period.Unit)
	}
}
