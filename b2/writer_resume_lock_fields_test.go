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
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func unfinishedWithLock(mode string, retainUntil int64, legalHold string) string {
	retention := `{"mode":null,"retainUntilTimestamp":null}`
	if mode != "" {
		retention = fmt.Sprintf(`{"mode":%q,"retainUntilTimestamp":%d}`, mode, retainUntil)
	}
	return fmt.Sprintf(`{"files":[{"fileId":"large-1","fileName":"resumed","action":"start","fileRetention":{"isClientAuthorizedToRead":true,"value":%s},"legalHold":{"isClientAuthorizedToRead":true,"value":%q}}],"nextFileId":null}`, retention, legalHold)
}

// Each Object Lock field of an unfinished upload is checked on resume on its
// own. TestWriterResumeChecksTheUnfinishedLock gets both fields wrong at once,
// so it cannot tell which check refused the resume.
func TestWriterResumeRefusesEachMismatchedLockField(t *testing.T) {
	const until = writerObjectLockRetainUntilMillis
	for _, tc := range []struct {
		name     string
		response string
		wantMsg  string
	}{
		{"legal hold only", unfinishedWithLock("compliance", until, "off"), "requested legal hold"},
		{"retention mode only", unfinishedWithLock("governance", until, "on"), "requested retention"},
		{"retain-until only", unfinishedWithLock("compliance", until+1000, "on"), "requested retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newObjectLockWire(t)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, tc.response)
			srv.Reply("b2_list_parts", http.StatusOK, `{"parts":[],"nextPartNumber":null}`)
			writer := bucket.Object("resumed").NewWriter(context.Background(), writerObjectLockOptions()...)
			writer.Resume = true
			// Force the resume path with a small fake-only chunk; real B2
			// requires non-final parts to be at least 5,000,000 bytes.
			writer.ChunkSize = 10
			_, writeErr := writer.Write([]byte("0123456789012345678901234"))
			closeErr := writer.Close()
			for op, err := range map[string]error{"Write": writeErr, "Close": closeErr} {
				if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("%s error = %v, want it to contain %q", op, err, tc.wantMsg)
				}
			}
			for _, call := range []string{"b2_list_parts", "b2_start_large_file", "upload_part", "b2_finish_large_file"} {
				if n := len(srv.Calls(call)); n != 0 {
					t.Errorf("%s calls = %d, want 0: a mismatched lock must not resume", call, n)
				}
			}
		})
	}
}
