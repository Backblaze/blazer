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
	"net/http"
	"strings"
	"testing"
)

func TestWriterResumeRejectsDuplicateUnfinishedUploads(t *testing.T) {
	for _, tc := range []struct{ name, files string }{
		{"A_then_B", `{"files":[{"fileId":"id-A","fileName":"resume","action":"start"},{"fileId":"id-B","fileName":"resume","action":"start"}],"nextFileId":null}`},
		{"B_then_A", `{"files":[{"fileId":"id-B","fileName":"resume","action":"start"},{"fileId":"id-A","fileName":"resume","action":"start"}],"nextFileId":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, tc.files)
			srv.Reply("b2_list_parts", http.StatusOK, `{"parts":[],"nextPartNumber":null}`)
			w := bucket.Object("resume").NewWriter(newTestCtx(t))
			w.Resume, w.ChunkSize = true, 1
			_, err := w.Write([]byte("abc"))
			if err == nil || !strings.Contains(err.Error(), "resume") || !strings.Contains(err.Error(), "more than one unfinished upload") || !strings.Contains(err.Error(), "cancel") || !strings.Contains(err.Error(), "id-A") || !strings.Contains(err.Error(), "id-B") {
				t.Errorf("Write error = %v, want duplicate upload error naming the object and both IDs", err)
			}
			_ = w.Close()
			for _, call := range []string{"b2_list_parts", "b2_get_upload_part_url", "upload_part", "b2_start_large_file", "b2_finish_large_file", "b2_cancel_large_file"} {
				if got := len(srv.Calls(call)); got != 0 {
					t.Errorf("%s requests = %d, want 0", call, got)
				}
			}
		})
	}
}
