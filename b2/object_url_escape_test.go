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
	"net/url"
	"strings"
	"testing"
	"time"
)

// Object names may contain '#', '?', '%' and spaces. AuthURL must carry the
// token as a query parameter and the name as the path, whatever the name holds.
// https://www.backblaze.com/apidocs/b2-download-file-by-name
func TestObjectAuthURLEscapesSpecialCharactersInName(t *testing.T) {
	for _, tc := range []struct {
		name, path string
	}{
		{"dir/a#b.txt", "/file/n/dir/a%23b.txt"},
		{"dir/a?x=1.txt", "/file/n/dir/a%3Fx=1.txt"},
		{"dir/a%zz.txt", "/file/n/dir/a%25zz.txt"},
		{"dir/a b%20c.txt", "/file/n/dir/a%20b%2520c.txt"},
	} {
		name := tc.name
		t.Run(name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			srv.Reply("b2_get_download_authorization", http.StatusOK, `{"authorizationToken":"download-token"}`)
			if got, want := bucket.Object(name).URL(), srv.URL()+tc.path; got != want {
				t.Errorf("Object.URL = %q, want %q", got, want)
			}
			u, err := bucket.Object(name).AuthURL(context.Background(), time.Hour, "")
			if err != nil {
				t.Fatalf("AuthURL: %v", err)
			}
			if got := u.EscapedPath(); got != tc.path {
				t.Errorf("escaped path = %q, want %q", got, tc.path)
			}
			if u.Fragment != "" {
				t.Errorf("fragment = %q, want none (the token would never be sent)", u.Fragment)
			}
			if got := u.Query().Get("Authorization"); got != "download-token" {
				t.Errorf("Authorization = %q, want %q (url %s)", got, "download-token", u)
			}
			if _, ok := u.Query()["x"]; ok {
				t.Errorf("name text leaked into the query string: %s", u)
			}
			if want := "/file/n/" + name; u.Path != want {
				t.Errorf("decoded path = %q, want %q", u.Path, want)
			}
		})
	}
}

func TestObjectURLsPreservePlus(t *testing.T) {
	srv, bucket := liveOnlyBucket(t)
	srv.Reply("b2_get_download_authorization", http.StatusOK, `{"authorizationToken":"download-token"}`)
	const name = "dir/a+b c/%2B+雪.txt"
	object := bucket.Object(name)
	auth, err := object.AuthURL(context.Background(), time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, value string }{{"URL", object.URL()}, {"AuthURL", auth.String()}} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			escaped := strings.TrimPrefix(u.EscapedPath(), "/file/n/")
			got, err := url.QueryUnescape(escaped)
			if err != nil {
				t.Fatal(err)
			}
			if got != name {
				t.Errorf("B2-decoded name = %q, want %q (escaped path %q)", got, name, escaped)
			}
			if want := "dir/a%2Bb%20c/%252B%2B%E9%9B%AA.txt"; escaped != want {
				t.Errorf("escaped path = %q, want %q", escaped, want)
			}
		})
	}
}
