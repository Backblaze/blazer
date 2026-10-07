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

func TestCopyFileRequests(t *testing.T) {
	var path string
	var sent map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_copy_file"):
			path = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			sent = map[string]any{}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"fileId":"new-id","fileName":"dst","accountId":"a","bucketId":"bkt","contentLength":5,"contentSha1":"x","contentType":"text/plain","fileInfo":{},"action":"copy","uploadTimestamp":1700000000000}`)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket := &Bucket{b2: b, ID: "bkt"}

	tests := []struct {
		name      string
		call      func() (*File, error)
		want      map[string]any
		wantNoKey []string
	}{
		{
			name: "COPY omits contentType and fileInfo even when given",
			call: func() (*File, error) {
				return bucket.CopyFile(context.Background(), "src", "dst", "", "", "COPY", "text/plain", map[string]string{"k": "v"})
			},
			want:      map[string]any{"sourceFileId": "src", "fileName": "dst", "metadataDirective": "COPY"},
			wantNoKey: []string{"contentType", "fileInfo", "destinationBucketId", "range"},
		},
		{
			name: "REPLACE sends contentType and fileInfo",
			call: func() (*File, error) {
				return bucket.CopyFile(context.Background(), "src", "dst", "", "", "REPLACE", "text/plain", map[string]string{"k": "v"})
			},
			want: map[string]any{"metadataDirective": "REPLACE", "contentType": "text/plain", "fileInfo": map[string]any{"k": "v"}},
		},
		{
			name: "REPLACE with no info sends an explicit empty fileInfo",
			call: func() (*File, error) {
				return bucket.CopyFile(context.Background(), "src", "dst", "", "", "REPLACE", "text/plain", nil)
			},
			want: map[string]any{"metadataDirective": "REPLACE", "contentType": "text/plain", "fileInfo": map[string]any{}},
		},
		{
			name: "destination bucket and range",
			call: func() (*File, error) {
				return bucket.CopyFile(context.Background(), "src", "dst", "other-bucket", "bytes=0-99", "COPY", "", nil)
			},
			want: map[string]any{"destinationBucketId": "other-bucket", "range": "bytes=0-99"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := tc.call()
			if err != nil {
				t.Fatalf("CopyFile: %v", err)
			}
			if path != "/b2api/v4/b2_copy_file" {
				t.Errorf("request path = %q, want /b2api/v4/b2_copy_file", path)
			}
			for k, want := range tc.want {
				if got := sent[k]; !reflect.DeepEqual(got, want) {
					t.Errorf("request %s = %v, want %v", k, got, want)
				}
			}
			for _, k := range tc.wantNoKey {
				if v, ok := sent[k]; ok {
					t.Errorf("request unexpectedly contained %s = %v", k, v)
				}
			}
			if f.ID != "new-id" || f.Name != "dst" || f.Status != "copy" || f.Size != 5 {
				t.Errorf("file = %+v, want the response decoded", f)
			}
		})
	}
}
