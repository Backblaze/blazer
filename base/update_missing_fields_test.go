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
)

// b2_update_bucket may omit fileLockConfiguration and replicationConfiguration
// (for example for a restricted key). Update must return a Bucket, not panic.
func TestUpdateToleratesMissingLockAndReplication(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			_, _ = fmt.Fprint(w, `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Bucket{b2: b, ID: "bid", Name: "n"}).Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
