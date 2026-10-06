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
	"strings"
	"testing"
)

func TestDeleteFileVersionBypassGovernanceOnTheWire(t *testing.T) {
	var sent map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_delete_file_version"):
			body, _ := io.ReadAll(r.Body)
			sent = map[string]any{}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode body: %v", err)
			}
			fmt.Fprint(w, `{"fileId":"id","fileName":"name"}`)
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
	file := (&Bucket{b2: b}).File("id", "name")

	tests := []struct {
		name       string
		call       func() error
		wantBypass bool
	}{
		{"no argument omits the field", func() error { return file.DeleteFileVersion(context.Background()) }, false},
		{"false omits the field", func() error { return file.DeleteFileVersion(context.Background(), false) }, false},
		{"true sends bypassGovernance true", func() error { return file.DeleteFileVersion(context.Background(), true) }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sent = nil
			if err := tc.call(); err != nil {
				t.Fatalf("DeleteFileVersion: %v", err)
			}
			if sent["fileId"] != "id" || sent["fileName"] != "name" {
				t.Errorf("request = %v, want fileId and fileName set", sent)
			}
			got, present := sent["bypassGovernance"]
			if tc.wantBypass {
				if got != true {
					t.Errorf("bypassGovernance = %v, want true", got)
				}
			} else if present {
				t.Errorf("bypassGovernance = %v, want the field omitted", got)
			}
		})
	}
}
