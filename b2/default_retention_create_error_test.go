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
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestNewBucketRetentionUpdateDenied(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deleteStatus int
		deleteBody   string
		wantBucket   bool
	}{
		{name: "cleanup succeeds", deleteStatus: http.StatusOK, deleteBody: `{"bucketId":"bid","bucketName":"n"}`},
		{name: "already absent", deleteStatus: http.StatusBadRequest, deleteBody: `{"status":400,"code":"bad_request","message":"Bucket n does not exist"}`},
		{name: "cleanup denied", deleteStatus: http.StatusForbidden, deleteBody: `{"status":403,"code":"access_denied","message":"delete denied"}`, wantBucket: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
			srv.Reply("b2_create_bucket", http.StatusOK, `{"bucketId":"bid","bucketName":"n","fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":null,"period":null}}}}`)
			srv.Reply("b2_update_bucket", http.StatusForbidden, `{"status":403,"code":"access_denied","message":"retention denied"}`)
			srv.Reply("b2_delete_bucket", tc.deleteStatus, tc.deleteBody)
			ctx := context.Background()
			client, err := NewClient(ctx, "account", "key", InsecureAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private, FileLockEnabled: true, DefaultRetention: &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 7, Unit: "days"}}})
			status, code, message := base.MsgCode(err)
			if status != 403 || code != "access_denied" || message != "retention denied" {
				t.Errorf("error = %v (%d, %q, %q), want 403 access_denied retention denied", err, status, code, message)
			}
			if (bucket != nil) != tc.wantBucket {
				t.Errorf("bucket = %v; want non-nil %v", bucket, tc.wantBucket)
			}
			if bucket != nil && bucket.ID() != "bid" {
				t.Errorf("remaining bucket ID = %q, want bid", bucket.ID())
			}
			if got := len(srv.Calls("b2_delete_bucket")); got != 1 {
				t.Errorf("cleanup calls = %d, want 1", got)
			}
		})
	}
}

func TestNewBucketRetentionCleanupAfterCancellation(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, `{"bucketId":"bid","bucketName":"n","fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":null,"period":null}}}}`)
	srv.Reply("b2_delete_bucket", http.StatusOK, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.Handle("b2_update_bucket", func(b2wiretest.Request) b2wiretest.Response {
		cancel()
		return b2wiretest.Response{Status: http.StatusForbidden, Body: `{"status":403,"code":"access_denied","message":"retention denied"}`}
	})
	client, err := NewClient(ctx, "account", "key", InsecureAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private, FileLockEnabled: true, DefaultRetention: &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 7, Unit: "days"}}})
	if err == nil || bucket != nil {
		t.Errorf("NewBucket returned bucket=%v, err=%v; want nil bucket and an error", bucket, err)
	}
	if got := len(srv.Calls("b2_delete_bucket")); got != 1 {
		t.Errorf("cleanup calls after cancellation = %d, want 1", got)
	}
}

func TestNewBucketRetentionDoesNotUpdateExistingBucket(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate"}]}`)
	client, err := NewClient(context.Background(), "account", "key", InsecureAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.NewBucket(context.Background(), "n", &BucketAttrs{DefaultRetention: &Retention{Mode: "governance", Period: &RetentionPeriod{Duration: 7, Unit: "days"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(srv.Calls("b2_create_bucket")) != 0 || len(srv.Calls("b2_update_bucket")) != 0 || len(srv.Calls("b2_delete_bucket")) != 0 {
		t.Fatal("NewBucket modified an existing bucket")
	}
}

func TestNewBucketWithoutRetentionDoesNotUpdate(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, updateBucketJSON)
	client, err := NewClient(context.Background(), "account", "key", InsecureAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.NewBucket(context.Background(), "n", &BucketAttrs{Type: Private}); err != nil {
		t.Fatal(err)
	}
	if got := len(srv.Calls("b2_update_bucket")); got != 0 {
		t.Fatalf("updates without default retention = %d, want 0", got)
	}
}
