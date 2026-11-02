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
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestUserAgentOptionReachesTheWire asserts the RFC 9110 User-Agent format:
// https://datatracker.ietf.org/doc/html/rfc9110#section-10.1.5
func TestUserAgentOptionReachesTheWire(t *testing.T) {
	const customUserAgent = "acme-app/1.2"
	var userAgents []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/b2api/v4/b2_authorize_account" {
			t.Errorf("path = %q, want b2_authorize_account", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		userAgents = append(userAgents, r.Header.Get("User-Agent"))
		_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
	}))
	defer srv.Close()

	for _, options := range [][]AuthOption{
		{SetAPIBase(srv.URL), UserAgent(customUserAgent)},
		{SetAPIBase(srv.URL)},
	} {
		if _, err := AuthorizeAccount(context.Background(), "account-id", "application-key", options...); err != nil {
			t.Fatal(err)
		}
	}
	if len(userAgents) != 2 {
		t.Fatalf("authorization requests = %d, want 2", len(userAgents))
	}
	prefix := customUserAgent + " "
	if !strings.HasPrefix(userAgents[0], prefix) || len(userAgents[0]) <= len(prefix) {
		t.Errorf("custom User-Agent = %q, want prefix %q followed by the default agent", userAgents[0], prefix)
	}
	if strings.Contains(userAgents[1], customUserAgent) {
		t.Errorf("default User-Agent = %q, want no %q", userAgents[1], customUserAgent)
	}
}

// TestMillitime asserts B2's millisecond upload timestamps:
// https://www.backblaze.com/apidocs/b2-list-file-names
func TestMillitime(t *testing.T) {
	for _, test := range []struct {
		milliseconds int64
		want         time.Time
	}{
		{0, time.Unix(0, 0)},
		{999, time.Unix(0, 999000000)},
		{1000, time.Unix(1, 0)},
		{1700000000123, time.Unix(1700000000, 123000000)},
	} {
		if got := millitime(test.milliseconds); !got.Equal(test.want) {
			t.Errorf("millitime(%d) = %v, want %v", test.milliseconds, got, test.want)
		}
	}
}

// TestDownloadUsesLargeFileSHA1WhenContentSHA1IsNone asserts B2's download response headers:
// https://www.backblaze.com/apidocs/b2-download-file-by-name
// https://www.backblaze.com/apidocs/b2-download-file-by-id
func TestDownloadUsesLargeFileSHA1WhenContentSHA1IsNone(t *testing.T) {
	const largeFileSHA1 = "0123456789abcdef0123456789abcdef01234567"
	const contentSHA1 = "89abcdef0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name, contentSHA1, largeFileSHA1, want string
	}{
		{"large file SHA-1", "none", largeFileSHA1, largeFileSHA1},
		{"content SHA-1 wins", contentSHA1, largeFileSHA1, contentSHA1},
		{"none without large file SHA-1", "none", "", "none"},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "0")
				w.Header().Set("X-Bz-Content-Sha1", test.contentSHA1)
				if test.largeFileSHA1 != "" {
					w.Header().Set("X-Bz-Info-large_file_sha1", test.largeFileSHA1)
				}
			}))
			defer srv.Close()

			bucket := &Bucket{Name: "bucket", b2: &B2{downloadURI: srv.URL, authToken: "token", opts: &b2Options{}}}
			for _, download := range []struct {
				name string
				fn   func() (*FileReader, error)
			}{
				{"by name", func() (*FileReader, error) {
					return bucket.DownloadFileByName(context.Background(), "name", 0, 0, false)
				}},
				{"by ID", func() (*FileReader, error) {
					return bucket.DownloadFileByID(context.Background(), "id", 0, 0, false)
				}},
			} {
				t.Run(download.name, func(t *testing.T) {
					reader, err := download.fn()
					if err != nil {
						t.Fatal(err)
					}
					if reader.SHA1 != test.want {
						t.Errorf("SHA1 = %q, want %q", reader.SHA1, test.want)
					}
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
