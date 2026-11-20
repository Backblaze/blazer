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
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Regression tests for defects that were reported by the known-defect suite and
// have since been fixed. Moved here from the known_defect_*_test.go files.

func newBaseWire(t *testing.T) (*b2wiretest.Server, *B2, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	srv := b2wiretest.New(t)
	root, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	return srv, root, ctx
}

const bucketJSONWithLock = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":7,` +
	`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false}}}`

// A bucket update reply without the file-lock section must not crash.
func TestRegression_UpdateBucketReplyWithoutFileLockDoesNotPanic(t *testing.T) {
	srv, root, ctx := newBaseWire(t)
	srv.Reply("b2_update_bucket", http.StatusOK, `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":7}`)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Update panicked on a reply without fileLockConfiguration: %v", r)
		}
	}()
	if _, err := (&Bucket{ID: "bid", Name: "n", b2: root}).Update(ctx); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
