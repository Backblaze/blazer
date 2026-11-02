package b2

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Pin the create request independently of a live account's bucket defaults.
// https://www.backblaze.com/apidocs/b2-create-bucket
func TestNewBucketAttributesWire(t *testing.T) {
	for _, test := range []struct {
		name      string
		attrs     *BucketAttrs
		info      any
		rules     any
		response  string
		wantInfo  map[string]string
		wantRules []LifecycleRule
	}{
		{
			name:     "no-attrs",
			response: `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1}`,
		},
		{
			name: "only-info", attrs: &BucketAttrs{Info: map[string]string{"this": "that", "other": "thing"}},
			info:     map[string]string{"this": "that", "other": "thing"},
			response: `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{"this":"that","other":"thing"},"lifecycleRules":[],"revision":1}`,
			wantInfo: map[string]string{"this": "that", "other": "thing"},
		},
		{
			name: "only-rules",
			attrs: &BucketAttrs{LifecycleRules: []LifecycleRule{
				{Prefix: "whee/", DaysHiddenUntilDeleted: 30},
				{Prefix: "whoa/", DaysNewUntilHidden: 1},
			}},
			rules: []map[string]any{
				{"fileNamePrefix": "whee/", "daysFromHidingToDeleting": 30},
				{"fileNamePrefix": "whoa/", "daysFromUploadingToHiding": 1},
			},
			response:  `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[{"fileNamePrefix":"whee/","daysFromHidingToDeleting":30},{"fileNamePrefix":"whoa/","daysFromUploadingToHiding":1}],"revision":1}`,
			wantRules: []LifecycleRule{{Prefix: "whee/", DaysHiddenUntilDeleted: 30}, {Prefix: "whoa/", DaysNewUntilHidden: 1}},
		},
		{
			name: "info-and-rules",
			attrs: &BucketAttrs{
				Info:           map[string]string{"this": "that", "other": "thing"},
				LifecycleRules: []LifecycleRule{{Prefix: "whee/", DaysHiddenUntilDeleted: 30}, {Prefix: "whoa/", DaysNewUntilHidden: 1}},
			},
			info: map[string]string{"this": "that", "other": "thing"},
			rules: []map[string]any{
				{"fileNamePrefix": "whee/", "daysFromHidingToDeleting": 30},
				{"fileNamePrefix": "whoa/", "daysFromUploadingToHiding": 1},
			},
			response:  `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{"this":"that","other":"thing"},"lifecycleRules":[{"fileNamePrefix":"whee/","daysFromHidingToDeleting":30},{"fileNamePrefix":"whoa/","daysFromUploadingToHiding":1}],"revision":1}`,
			wantInfo:  map[string]string{"this": "that", "other": "thing"},
			wantRules: []LifecycleRule{{Prefix: "whee/", DaysHiddenUntilDeleted: 30}, {Prefix: "whoa/", DaysNewUntilHidden: 1}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			srv.ReplySequence("b2_list_buckets",
				b2wiretest.Response{Status: http.StatusOK, Body: `{"buckets":[]}`},
				b2wiretest.Response{Status: http.StatusOK, Body: `{"buckets":[` + test.response + `]}`},
			)
			srv.Reply("b2_create_bucket", http.StatusOK, test.response)
			ctx := context.Background()
			client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.NewBucket(ctx, "n", test.attrs)
			if err != nil {
				t.Fatal(err)
			}
			if bucket.Name() != "n" || bucket.ID() != "bid" {
				t.Fatalf("created bucket name/id = %q/%q, want n/bid", bucket.Name(), bucket.ID())
			}
			creates := srv.Calls("b2_create_bucket")
			if len(creates) != 1 {
				t.Fatalf("create calls = %d, want 1", len(creates))
			}
			request := creates[0]
			b2wiretest.AssertJSON(t, request, "accountId", "a")
			b2wiretest.AssertJSON(t, request, "bucketName", "n")
			b2wiretest.AssertJSON(t, request, "bucketType", "allPrivate")
			b2wiretest.AssertJSON(t, request, "bucketInfo", test.info)
			b2wiretest.AssertJSON(t, request, "lifecycleRules", test.rules)
			b2wiretest.AssertNoJSON(t, request, "defaultServerSideEncryption")
			attrs, err := bucket.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(attrs.Info) != len(test.wantInfo) {
				t.Errorf("Attrs Info has %d entries, want %d", len(attrs.Info), len(test.wantInfo))
			}
			for key, want := range test.wantInfo {
				if got := attrs.Info[key]; got != want {
					t.Errorf("Attrs Info[%q] = %q, want %q", key, got, want)
				}
			}
			if len(attrs.LifecycleRules) != len(test.wantRules) {
				t.Fatalf("Attrs LifecycleRules = %d rules, want %d", len(attrs.LifecycleRules), len(test.wantRules))
			}
			for i, want := range test.wantRules {
				got := attrs.LifecycleRules[i]
				if got.Prefix != want.Prefix {
					t.Errorf("Attrs LifecycleRules[%d].Prefix = %q, want %q", i, got.Prefix, want.Prefix)
				}
				if got.DaysNewUntilHidden != want.DaysNewUntilHidden {
					t.Errorf("Attrs LifecycleRules[%d].DaysNewUntilHidden = %d, want %d", i, got.DaysNewUntilHidden, want.DaysNewUntilHidden)
				}
				if got.DaysHiddenUntilDeleted != want.DaysHiddenUntilDeleted {
					t.Errorf("Attrs LifecycleRules[%d].DaysHiddenUntilDeleted = %d, want %d", i, got.DaysHiddenUntilDeleted, want.DaysHiddenUntilDeleted)
				}
			}
		})
	}
}

// Omitted SSE must stay omitted; decode the service's answer rather than
// assuming an account default. The null setting also covers older buckets.
// https://www.backblaze.com/apidocs/b2-create-bucket
func TestNewBucketDefaultEncryptionWire(t *testing.T) {
	for _, test := range []struct {
		name    string
		attrs   *BucketAttrs
		service string
		want    *ServerSideEncryption
	}{
		{
			name: "sse-omitted-encrypted", service: `{"mode":"SSE-B2","algorithm":"AES256"}`,
			want: &ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"},
		},
		{name: "sse-omitted-legacy-unencrypted", service: `{"mode":null,"algorithm":null}`},
		{
			name: "sse-explicit", attrs: &BucketAttrs{DefaultServerSideEncryption: SSEB2WithAES256()},
			service: `{"mode":"SSE-B2","algorithm":"AES256"}`,
			want:    &ServerSideEncryption{Mode: "SSE-B2", Algorithm: "AES256"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := b2wiretest.New(t)
			bucketJSON := fmt.Sprintf(`{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1,"defaultServerSideEncryption":{"isClientAuthorizedToRead":true,"value":%s}}`, test.service)
			srv.ReplySequence("b2_list_buckets",
				b2wiretest.Response{Body: `{"buckets":[]}`},
				b2wiretest.Response{Body: `{"buckets":[` + bucketJSON + `]}`},
			)
			srv.Reply("b2_create_bucket", http.StatusOK, bucketJSON)
			ctx := context.Background()
			client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.NewBucket(ctx, "n", test.attrs)
			if err != nil {
				t.Fatal(err)
			}
			creates := srv.Calls("b2_create_bucket")
			if len(creates) != 1 {
				t.Fatalf("create calls = %d, want 1", len(creates))
			}
			if test.attrs == nil {
				b2wiretest.AssertNoJSON(t, creates[0], "defaultServerSideEncryption")
			} else {
				b2wiretest.AssertJSON(t, creates[0], "defaultServerSideEncryption", map[string]string{"mode": "SSE-B2", "algorithm": "AES256"})
			}
			attrs, err := bucket.Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(attrs.DefaultServerSideEncryption, test.want) {
				t.Errorf("default encryption = %+v, want %+v", attrs.DefaultServerSideEncryption, test.want)
			}
		})
	}
}
