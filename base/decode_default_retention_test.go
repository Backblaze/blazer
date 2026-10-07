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
	"reflect"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2types"
)

func retentionCfg(mode, unit *string, duration int) *b2types.FileLockConfiguration {
	cfg := &b2types.FileLockConfiguration{}
	cfg.Val.DefaultRetention.Mode = mode
	cfg.Val.DefaultRetention.Period.Unit = unit
	cfg.Val.DefaultRetention.Period.Duration = duration
	return cfg
}

func TestDecodeDefaultRetention(t *testing.T) {
	gov, days := "governance", "days"
	tests := []struct {
		name string
		cfg  *b2types.FileLockConfiguration
		want *b2types.Retention
	}{
		{"absent configuration (key cannot read it)", nil, nil},
		{"no default retention (null mode)", retentionCfg(nil, nil, 0), nil},
		{"mode without a unit does not panic", retentionCfg(&gov, nil, 7), nil},
		{"unit without a mode", retentionCfg(nil, &days, 7), nil},
		{"mode and period", retentionCfg(&gov, &days, 30), &b2types.Retention{Mode: "governance", Period: &b2types.RetentionPeriod{Duration: 30, Unit: "days"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeDefaultRetention(tc.cfg); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decodeDefaultRetention = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Update used to dereference the period unit without a nil check.
func TestUpdateToleratesDefaultRetentionWithoutUnit(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			_, _ = fmt.Fprint(w, `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,`+
				`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":true,"defaultRetention":{"mode":"governance","period":{"duration":7,"unit":null}}}},`+
				`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`)
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
	updated, err := (&Bucket{b2: b, ID: "bid", Name: "n"}).Update(context.Background())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.DefaultRetention != nil {
		t.Errorf("DefaultRetention = %+v, want nil for a response without a period unit", updated.DefaultRetention)
	}
}
