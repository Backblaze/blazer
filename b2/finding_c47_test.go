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
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Promotes TestKnownDefect_NewBucketDoesNotIgnoreDefaultRetention into the regular suite.
// B2 sets default retention through b2_update_bucket.
// https://www.backblaze.com/apidocs/b2-update-bucket
func TestRegression_NewBucketDoesNotIgnoreDefaultRetention(t *testing.T) {
	const without = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":null,"period":null}}}}`
	const with = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":"governance","period":{"duration":7,"unit":"days"}}}}}`
	srv := b2wiretest.New(t)
	srv.Reply("b2_create_bucket", http.StatusOK, without)
	var mu sync.Mutex
	created, retained := false, false
	srv.Handle("b2_list_buckets", func(b2wiretest.Request) b2wiretest.Response {
		mu.Lock()
		defer mu.Unlock()
		if !created {
			created = true
			return b2wiretest.Response{Body: `{"buckets":[]}`}
		}
		if retained {
			return b2wiretest.Response{Body: `{"buckets":[` + with + `]}`}
		}
		return b2wiretest.Response{Body: `{"buckets":[` + without + `]}`}
	})
	srv.Handle("b2_update_bucket", func(req b2wiretest.Request) b2wiretest.Response {
		b2wiretest.AssertJSON(t, req, "defaultRetention", map[string]any{"mode": "governance", "period": map[string]any{"duration": 7, "unit": "days"}})
		mu.Lock()
		retained = true
		mu.Unlock()
		return b2wiretest.Response{Body: with}
	})
	ctx := context.Background()
	client, err := NewClient(ctx, "account", "key", InsecureAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private, FileLockEnabled: true, DefaultRetention: &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 7, Unit: "days"}}})
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.DefaultRetention == nil || attrs.DefaultRetention.Mode != "governance" || attrs.DefaultRetention.Period == nil || attrs.DefaultRetention.Period.Duration != 7 || attrs.DefaultRetention.Period.Unit != "days" {
		t.Fatalf("created bucket default retention = %+v, want governance for 7 days", attrs.DefaultRetention)
	}
}
