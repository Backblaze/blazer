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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// v4AuthJSON builds a v4-shaped authorizeAccount response whose apiUrl points
// back at the test server. bucketIDs/bucketNames (equal length) are zipped into
// allowed.buckets. allowed is always present; an empty scope carries null
// buckets and namePrefix, the shape B2 returns for master keys.
func v4AuthJSON(apiURL string, bucketIDs, bucketNames []string, namePrefix string) string {
	var buckets []map[string]any // nil marshals to null, the master-key shape
	for i, id := range bucketIDs {
		buckets = append(buckets, map[string]any{"id": id, "name": bucketNames[i]})
	}
	var prefix any // null when unset
	if namePrefix != "" {
		prefix = namePrefix
	}
	storageAPI := map[string]any{
		"absoluteMinimumPartSize": 5000000,
		"apiUrl":                  apiURL,
		"capabilities":            []string{"readFiles", "writeFiles"},
		"downloadUrl":             apiURL,
		"storageApi":              "storage",
		"recommendedPartSize":     100000000,
		"s3ApiUrl":                apiURL,
		"allowed": map[string]any{
			"buckets":      buckets,
			"capabilities": []string{"readFiles", "writeFiles"},
			"namePrefix":   prefix,
		},
	}
	resp := map[string]any{
		"accountId":                         "account-id",
		"authorizationToken":                "auth-token",
		"applicationKeyExpirationTimestamp": 0,
		"apiInfo": map[string]any{
			"storageApi": storageAPI,
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestAuthorizeAccountV4(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b2api/v4/b2_authorize_account" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		fmt.Fprint(w, v4AuthJSON(srv.URL, []string{"buck-a", "buck-b"}, []string{"name-a", "name-b"}, "restic/"))
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	if want := []string{"buck-a", "buck-b"}; !reflect.DeepEqual(b.buckets, want) {
		t.Errorf("buckets = %v, want %v", b.buckets, want)
	}
	if b.pfx != "restic/" {
		t.Errorf("pfx = %q, want %q", b.pfx, "restic/")
	}
	if b.apiURI != srv.URL {
		t.Errorf("apiURI = %q, want %q", b.apiURI, srv.URL)
	}
	if b.accountID != "account-id" {
		t.Errorf("accountID = %q, want %q", b.accountID, "account-id")
	}
}

func TestAuthorizeAccountV4UnrestrictedKey(t *testing.T) {
	// Master keys still carry an allowed block, but with null buckets and
	// namePrefix.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	if len(b.buckets) != 0 {
		t.Errorf("buckets = %v, want empty", b.buckets)
	}
	if b.pfx != "" {
		t.Errorf("pfx = %q, want empty", b.pfx)
	}
}

// TestAuthorizeAccountV4ReadsAllowedNesting pins the fix: v4 scope lives under
// storageApi.allowed, so top-level v3-style bucketIds/namePrefix are ignored.
func TestAuthorizeAccountV4ReadsAllowedNesting(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Scope at the (wrong) v3 top level, with no allowed block.
		resp := map[string]any{
			"accountId":          "account-id",
			"authorizationToken": "auth-token",
			"apiInfo": map[string]any{
				"storageApi": map[string]any{
					"apiUrl":      srv.URL,
					"downloadUrl": srv.URL,
					"s3ApiUrl":    srv.URL,
					// v3-style fields that v4 no longer emits:
					"bucketIds":   []string{"buck-a"},
					"bucketNames": []string{"name-a"},
					"namePrefix":  "restic/",
				},
			},
		}
		b, _ := json.Marshal(resp)
		fmt.Fprint(w, string(b))
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	if len(b.buckets) != 0 {
		t.Errorf("buckets = %v, want empty (top-level bucketIds must be ignored in v4)", b.buckets)
	}
	if b.pfx != "" {
		t.Errorf("pfx = %q, want empty (top-level namePrefix must be ignored in v4)", b.pfx)
	}
}

// TestAuthorizeAccountV4SingleBucketRestrictedKey checks the headline case: a
// restricted key's nested scope is surfaced so the client knows its bucket.
func TestAuthorizeAccountV4SingleBucketRestrictedKey(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, v4AuthJSON(srv.URL, []string{"buck-a"}, []string{"name-a"}, ""))
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	if want := []string{"buck-a"}; !reflect.DeepEqual(b.buckets, want) {
		t.Errorf("buckets = %v, want %v", b.buckets, want)
	}
}

// TestUpdateCarriesScope guards re-auth: a re-authorized B2 must keep the
// key's bucket and prefix restrictions, or scope-aware calls like ListBuckets
// silently regress to unrestricted behavior.
func TestUpdateCarriesScope(t *testing.T) {
	b := &B2{}
	b.Update(&B2{
		accountID: "account-id",
		buckets:   []string{"buck-a", "buck-b"},
		pfx:       "restic/",
	})
	if want := []string{"buck-a", "buck-b"}; !reflect.DeepEqual(b.buckets, want) {
		t.Errorf("buckets = %v, want %v", b.buckets, want)
	}
	if b.pfx != "restic/" {
		t.Errorf("pfx = %q, want %q", b.pfx, "restic/")
	}
}

// TestListBucketsFansOutForMultiBucketKeys pins the fan-out: b2_list_buckets
// rejects any unfiltered request from a restricted key with 401 and accepts at
// most one bucketId filter, so a multi-bucket key must issue one filtered
// request per allowed bucket and merge the results.
func TestListBucketsFansOutForMultiBucketKeys(t *testing.T) {
	bucketNames := map[string]string{"buck-a": "name-a", "buck-b": "name-b"}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			fmt.Fprint(w, v4AuthJSON(srv.URL, []string{"buck-a", "buck-b"}, []string{"name-a", "name-b"}, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			req := map[string]any{}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("decode body: %v", err)
			}
			id, _ := req["bucketId"].(string)
			if id == "" {
				// Live B2 rejects unfiltered listing from restricted keys.
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"status": 401, "code": "unauthorized", "message": ""}`)
				return
			}
			name, ok := bucketNames[id]
			if !ok {
				t.Errorf("filtered list for unexpected bucketId %q", id)
			}
			fmt.Fprintf(w, `{"buckets": [{"bucketId": %q, "bucketName": %q, "bucketType": "allPrivate"}]}`, id, name)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	buckets, err := b.ListBuckets(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	var names []string
	for _, bucket := range buckets {
		names = append(names, bucket.Name)
	}
	if want := []string{"name-a", "name-b"}; !reflect.DeepEqual(names, want) {
		t.Errorf("ListBuckets = %v, want %v", names, want)
	}
}

// createKeyFixture serves authorize_account, then captures the single
// b2_create_key request (path, method, body) for the caller to inspect.
type createKeyFixture struct {
	b2         *B2
	srv        *httptest.Server
	lastPath   string
	lastBody   map[string]any
	lastMethod string
}

func newCreateKeyFixture(t *testing.T) *createKeyFixture {
	t.Helper()
	f := &createKeyFixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			fmt.Fprint(w, v4AuthJSON(f.srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_create_key"):
			f.lastPath = r.URL.Path
			f.lastMethod = r.Method
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			f.lastBody = map[string]any{}
			if err := json.Unmarshal(body, &f.lastBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			// Minimal Key response.
			fmt.Fprint(w, `{"applicationKeyId":"k","applicationKey":"s","accountId":"a","capabilities":[],"keyName":"n","expirationTimestamp":0}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(f.srv.URL))
	if err != nil {
		t.Fatalf("AuthorizeAccount: %v", err)
	}
	f.b2 = b
	return f
}

func (f *createKeyFixture) close() { f.srv.Close() }

func TestCreateKeyUsesV3Endpoint(t *testing.T) {
	f := newCreateKeyFixture(t)
	defer f.close()

	if _, err := f.b2.CreateKey(context.Background(), "keyname", []string{"readFiles"}, 0, "buck-single", "prefix/"); err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if want := "/b2api/v3/b2_create_key"; f.lastPath != want {
		t.Errorf("path = %q, want %q", f.lastPath, want)
	}
	if f.lastMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", f.lastMethod)
	}
	if got, want := f.lastBody["bucketId"], "buck-single"; got != want {
		t.Errorf("request body bucketId = %v, want %q", got, want)
	}
	if _, ok := f.lastBody["bucketIds"]; ok {
		t.Errorf("request body unexpectedly contained bucketIds: %v", f.lastBody["bucketIds"])
	}
}

func TestCreateKeyMultiBucketUsesV4Endpoint(t *testing.T) {
	f := newCreateKeyFixture(t)
	defer f.close()

	if _, err := f.b2.CreateKeyMultiBucket(context.Background(), "keyname", []string{"readFiles"}, 0, []string{"buck-a", "buck-b"}, "prefix/"); err != nil {
		t.Fatalf("CreateKeyMultiBucket: %v", err)
	}
	if want := "/b2api/v4/b2_create_key"; f.lastPath != want {
		t.Errorf("path = %q, want %q", f.lastPath, want)
	}
	if f.lastMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", f.lastMethod)
	}
	got, ok := f.lastBody["bucketIds"].([]any)
	if !ok {
		t.Fatalf("request body bucketIds missing or wrong type: %v", f.lastBody["bucketIds"])
	}
	want := []any{"buck-a", "buck-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request body bucketIds = %v, want %v", got, want)
	}
	if _, ok := f.lastBody["bucketId"]; ok {
		t.Errorf("request body unexpectedly contained bucketId: %v", f.lastBody["bucketId"])
	}
}
