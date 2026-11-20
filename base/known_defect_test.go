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

//go:build knowndefect

package base

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// The handle returned by a successful update carries the server's
// new revision, so the next update has a conflict check.
func TestKnownDefect_UpdateReturnsNewRevision(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_update_bucket", http.StatusOK, bucketJSONWithLock)
	updated, err := (&Bucket{ID: "bid", Name: "n", b2: root, rev: 2}).Update(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if updated.rev != 7 {
		t.Errorf("updated bucket revision = %d, want 7 (the revision in the reply)", updated.rev)
	}
	// And the next update must send it as ifRevisionIs.
	if _, err := updated.Update(ctx); err != nil {
		t.Fatal(err)
	}
	b2wiretest.AssertJSON(t, srv.Last(t, "b2_update_bucket"), "ifRevisionIs", 7)
}

// A non-JSON error reply (for example a gateway error page) must
// still produce a non-empty error message.
func TestKnownDefect_NonJSONErrorReplyHasMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><body>Forbidden by gateway</body></html>"))
	}))
	defer srv.Close()
	_, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), "b2_authorize_account: 403:"))
	if msg == "" {
		t.Errorf("error message is empty after the status code: %q", err.Error())
	}
}

// The size reported for a large file must not include the 40-byte
// SHA1 trailer of each streamed part.
func TestKnownDefect_LargeFileSizeExcludesSHA1Trailer(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"large","action":"upload"}`)
	large := (&File{ID: "large-id", b2: root}).AsLargeFile()
	chunk, err := large.GetUploadPartURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// "abc" plus its 40-hex SHA1: the streamed form, 43 bytes on the wire.
	body := "abca9993e364706816aba3e25717850c26c9cd0d89d"
	if _, err := chunk.UploadPart(ctx, strings.NewReader(body), "hex_digits_at_end", len(body), 1); err != nil {
		t.Fatal(err)
	}
	file, err := large.FinishLargeFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if file.Size != 3 {
		t.Errorf("finished large file size = %d, want 3 (payload only, 40-byte trailer excluded)", file.Size)
	}
}
