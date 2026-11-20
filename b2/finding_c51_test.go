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
	"errors"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestFindingC51_RevealReturnsListingError(t *testing.T) {
	t.Run("cancelled context", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := bucket.Reveal(ctx, "gone")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Reveal error = %v, want context.Canceled", err)
		}
		if IsNotExist(err) {
			t.Errorf("cancelled listing reported not found: %v", err)
		}
		if got := len(srv.Calls("b2_list_file_versions")); got != 0 {
			t.Errorf("list calls = %d, want 0 for a cancelled context", got)
		}
		if got := len(srv.Calls("b2_delete_file_version")); got != 0 {
			t.Errorf("delete calls = %d, want 0", got)
		}
	})

	for _, tc := range []struct {
		name      string
		firstPage bool
		wantCalls int
	}{
		{name: "permission error on first page", wantCalls: 1},
		{name: "permission error after empty page", firstPage: true, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			denied := b2wiretest.Response{Status: http.StatusForbidden, Body: `{"status":403,"code":"access_denied","message":"listing denied"}`}
			if tc.firstPage {
				srv.ReplySequence("b2_list_file_versions", b2wiretest.Response{Status: http.StatusOK, Body: `{"files":[],"nextFileName":"gone","nextFileId":"cursor"}`}, denied)
			} else {
				srv.ReplySequence("b2_list_file_versions", denied)
			}
			err := bucket.Reveal(context.Background(), "gone")
			if status, code, message := base.MsgCode(err); status != http.StatusForbidden || code != "access_denied" || message != "listing denied" {
				t.Errorf("Reveal error = %v (status %d, code %q, message %q), want 403 access_denied: listing denied", err, status, code, message)
			}
			if IsNotExist(err) {
				t.Errorf("refused listing reported not found: %v", err)
			}
			calls := srv.Calls("b2_list_file_versions")
			if len(calls) != tc.wantCalls {
				t.Fatalf("list calls = %d, want %d", len(calls), tc.wantCalls)
			}
			for _, call := range calls {
				b2wiretest.AssertJSON(t, call, "prefix", "gone")
			}
			if tc.firstPage {
				b2wiretest.AssertJSON(t, calls[1], "startFileName", "gone")
				b2wiretest.AssertJSON(t, calls[1], "startFileId", "cursor")
			}
			if got := len(srv.Calls("b2_delete_file_version")); got != 0 {
				t.Errorf("delete calls = %d, want 0", got)
			}
		})
	}

	t.Run("empty listing remains not found", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		srv.Reply("b2_list_file_versions", http.StatusOK, `{"files":[],"nextFileName":null,"nextFileId":null}`)
		if err := bucket.Reveal(context.Background(), "gone"); !IsNotExist(err) {
			t.Errorf("Reveal error = %v, want not found", err)
		}
		if got := len(srv.Calls("b2_list_file_versions")); got != 1 {
			t.Errorf("list calls = %d, want 1", got)
		}
		if got := len(srv.Calls("b2_delete_file_version")); got != 0 {
			t.Errorf("delete calls = %d, want 0", got)
		}
	})
}
