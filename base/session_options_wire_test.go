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
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const downloadHeadResponseID = "file-id"

// newOptionsWire starts a recording server that answers the calls the option
// tests make: authorize, list buckets and a download by ID.
func newOptionsWire(t *testing.T) *b2wiretest.Server {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Handle("b2_download_file_by_id", func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.Response{Status: http.StatusOK, Header: http.Header{
			"Content-Length": {"0"},
			"X-Bz-File-Id":   {downloadHeadResponseID},
			"X-Bz-File-Name": {"name"},
		}}
	})
	return srv
}

// authorizeAndCall runs b2_authorize_account, a JSON call and a download with
// the options, and returns the request for each.
func authorizeAndCall(t *testing.T, opts ...AuthOption) (auth, list, download b2wiretest.Request) {
	t.Helper()
	srv := newOptionsWire(t)
	ctx := context.Background()
	b, err := AuthorizeAccount(ctx, "account", "key", append([]AuthOption{SetAPIBase(srv.URL())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ListBuckets(ctx, ""); err != nil {
		t.Fatal(err)
	}
	reader, err := (&Bucket{b2: b}).DownloadFileByID(ctx, downloadHeadResponseID, 0, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return srv.Last(t, "b2_authorize_account"), srv.Last(t, "b2_list_buckets"), srv.Last(t, "b2_download_file_by_id")
}

// The B2 service reads these headers to inject failures for client testing:
// https://www.backblaze.com/docs/cloud-storage-integration-checklist.
func TestTestModeOptionsSendHeaderOnEveryRequestKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []AuthOption
		want []string
	}{
		{"none", nil, nil},
		{"fail some uploads", []AuthOption{FailSomeUploads()}, []string{"fail_some_uploads"}},
		{"expire some tokens", []AuthOption{ExpireSomeAuthTokens()}, []string{"expire_some_account_authorization_tokens"}},
		{"force cap exceeded", []AuthOption{ForceCapExceeded()}, []string{"force_cap_exceeded"}},
		{"all three", []AuthOption{ForceCapExceeded(), FailSomeUploads(), ExpireSomeAuthTokens()},
			[]string{"fail_some_uploads", "expire_some_account_authorization_tokens", "force_cap_exceeded"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, list, download := authorizeAndCall(t, tc.opts...)
			for _, r := range []b2wiretest.Request{auth, list, download} {
				if got := r.Header.Values("X-Bz-Test-Mode"); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s X-Bz-Test-Mode = %q, want %q", r.Call, got, tc.want)
				}
			}
		})
	}
}

func TestUserAgentOptionsArePrependedToTheDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []AuthOption
		want string
	}{
		{"default", nil, DefaultUserAgent},
		{"one", []AuthOption{UserAgent("app/1")}, "app/1 " + DefaultUserAgent},
		{"later options come first", []AuthOption{UserAgent("app/1"), UserAgent("lib/2")}, "lib/2 app/1 " + DefaultUserAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, list, download := authorizeAndCall(t, tc.opts...)
			for _, r := range []b2wiretest.Request{auth, list, download} {
				b2wiretest.AssertHeader(t, r, "User-Agent", tc.want)
			}
		})
	}
}

func TestBucketURLsComeFromTheAuthorizeResponse(t *testing.T) {
	srv := b2wiretest.New(t)
	srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(`{"accountId":"a","authorizationToken":"t","applicationKeyExpirationTimestamp":0,"apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"apiUrl":%q,"capabilities":[],"downloadUrl":"https://download.example.test","storageApi":"storage","recommendedPartSize":100000000,"s3ApiUrl":"https://s3.example.test","allowed":{"buckets":null,"capabilities":[],"namePrefix":null}}}}`, srv.URL()))
	b, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket := &Bucket{b2: b}
	if got, want := bucket.BaseURL(), "https://download.example.test"; got != want {
		t.Errorf("BaseURL() = %q, want %q", got, want)
	}
	if got, want := bucket.S3URL(), "https://s3.example.test"; got != want {
		t.Errorf("S3URL() = %q, want %q", got, want)
	}
}
