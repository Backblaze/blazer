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
	"io/fs"
	"net/http"
	"testing"
)

func TestGetFileInfoByNameNotFoundUnwraps(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_list_file_names", http.StatusOK, `{"files":[],"nextFileName":null}`)
	attrs, err := bucket.GetFileInfoByName(context.Background(), "missing")
	if attrs != nil || !IsNotExist(err) {
		t.Fatalf("GetFileInfoByName = (%v, %v), want a not-found error", attrs, err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = false, want true", err)
	}
	if errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(%v, fs.ErrPermission) = true, want false", err)
	}
}
