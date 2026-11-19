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
	"io"
	"net/http"
	"testing"
	"time"
)

type failingUploadSource struct {
	data     []byte
	err      error
	sameRead bool
}

func (r *failingUploadSource) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if r.sameRead && len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestFindingC5_PartialSourceIsNotCommittedByClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chunk    int
		sameRead bool
	}{
		{name: "small", chunk: 100},
		{name: "small error with bytes", chunk: 100, sameRead: true},
		{name: "multipart", chunk: 3},
		{name: "multipart error with bytes", chunk: 3, sameRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"partial","action":"upload"}`)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			writer := bucket.Object("partial").NewWriter(ctx)
			// The recording server accepts subminimum chunks to exercise multipart locally.
			writer.ChunkSize = tc.chunk
			writer.ConcurrentUploads = 1
			want := errors.New("source failed halfway")
			payload := []byte("partial-data")
			source := &failingUploadSource{data: payload, err: want, sameRead: tc.sameRead}
			n, copyErr := io.Copy(writer, source)
			// Reproduce the README's old cleanup pattern without cancelling first.
			closeErr := writer.Close()
			t.Logf("io.Copy = (%d, %v); Close = %v; upload_file = %d; finish_large_file = %d", n, copyErr, closeErr, len(srv.Calls("upload_file")), len(srv.Calls("b2_finish_large_file")))
			if n != int64(len(payload)) || !errors.Is(copyErr, want) {
				t.Fatalf("io.Copy = (%d, %v), want (%d, source error)", n, copyErr, len(payload))
			}
			if !errors.Is(closeErr, want) {
				t.Errorf("Close = %v, want source error", closeErr)
			}
			if uploads := srv.Calls("upload_file"); len(uploads) != 0 {
				t.Errorf("source error committed %d single-file uploads; partial body = %q", len(uploads), uploads[0].Body)
			}
			if finishes := srv.Calls("b2_finish_large_file"); len(finishes) != 0 {
				t.Errorf("source error committed %d multipart uploads", len(finishes))
			}
			if tc.chunk == 3 && (len(srv.Calls("b2_start_large_file")) != 1 || len(srv.Calls("upload_part")) == 0) {
				t.Error("test did not exercise an in-progress multipart upload")
			}
		})
	}
}
