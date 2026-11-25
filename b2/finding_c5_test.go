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

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

type failingUploadSource struct {
	data        []byte
	err         error
	sameRead    bool
	beforeError func() error
}

func (r *failingUploadSource) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.beforeError != nil {
			if err := r.beforeError(); err != nil {
				return 0, err
			}
		}
		return 0, r.err
	}
	data := r.data
	if r.sameRead && len(data) > 1 {
		data = data[:len(data)-1]
	}
	if r.sameRead && len(r.data) == 1 && r.beforeError != nil {
		if err := r.beforeError(); err != nil {
			return 0, err
		}
	}
	n := copy(p, data)
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
			if tc.chunk == 3 {
				partsRead := make(chan struct{}, 3)
				srv.Handle("upload_part", func(b2wiretest.Request) b2wiretest.Response {
					partsRead <- struct{}{}
					return b2wiretest.Response{Body: `{}`}
				})
				// Let the helper receive dispatched bodies before cancellation; the
				// last part remains buffered when the source reports its error.
				source.beforeError = func() error {
					for range 3 {
						select {
						case <-partsRead:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return nil
				}
			}
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

type failingUploadWriterTo struct {
	failingUploadSource
	used bool
}

func (r *failingUploadWriterTo) WriteTo(w io.Writer) (int64, error) {
	r.used = true
	n, err := w.Write(r.data)
	if err != nil {
		return int64(n), err
	}
	if r.beforeError != nil {
		if err := r.beforeError(); err != nil {
			return int64(n), err
		}
	}
	return int64(n), r.err
}

func TestUploadCopyErrorCancelsWriterTo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chunk int
	}{
		{name: "small", chunk: 100},
		{name: "multipart", chunk: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"writer-to","action":"upload"}`)
			parent, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(parent)
			defer cancel()
			w := bucket.Object("writer-to").NewWriter(ctx)
			// Subminimum chunks force multipart against the recording server.
			w.ChunkSize = tc.chunk
			w.ConcurrentUploads = 1
			want := errors.New("source WriterTo failed")
			source := &failingUploadWriterTo{failingUploadSource: failingUploadSource{data: []byte("partial-data"), err: want}}
			if tc.chunk == 3 {
				partsRead := make(chan struct{}, 3)
				srv.Handle("upload_part", func(b2wiretest.Request) b2wiretest.Response {
					partsRead <- struct{}{}
					return b2wiretest.Response{Body: `{}`}
				})
				source.beforeError = func() error {
					for range 3 {
						select {
						case <-partsRead:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return nil
				}
			}
			n, copyErr := io.Copy(w, source)
			if copyErr != nil {
				cancel() // Cancel the parent before Close cleans up.
			}
			closeErr := w.Close()
			if !source.used || n != 12 || !errors.Is(copyErr, want) {
				t.Fatalf("WriterTo used = %t, io.Copy = (%d, %v); want true, 12, source error", source.used, n, copyErr)
			}
			if !errors.Is(closeErr, context.Canceled) {
				t.Errorf("Close = %v, want canceled upload", closeErr)
			}
			if uploads := srv.Calls("upload_file"); len(uploads) != 0 {
				t.Errorf("cancelled copy committed %d uploads", len(uploads))
			}
			if finishes := srv.Calls("b2_finish_large_file"); len(finishes) != 0 {
				t.Errorf("cancelled copy finalized %d multipart uploads", len(finishes))
			}
			if tc.chunk == 3 && (len(srv.Calls("b2_start_large_file")) != 1 || len(srv.Calls("upload_part")) != 3) {
				t.Error("test did not exercise an in-progress multipart upload")
			}
		})
	}
}
