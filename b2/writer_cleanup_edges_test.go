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
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestWriterNegativeChunkSizeRejectsBeforeUpload(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*Writer) error
	}{
		{"Write", func(w *Writer) error { _, err := w.Write([]byte("hello")); return err }},
		{"ReadFrom seekable", func(w *Writer) error { _, err := w.ReadFrom(bytes.NewReader([]byte("hello"))); return err }},
		{"ReadFrom stream", func(w *Writer) error {
			_, err := w.ReadFrom(struct{ io.Reader }{strings.NewReader("hello")})
			return err
		}},
		{"Close empty", func(w *Writer) error { return w.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"negative","action":"upload"}`)
			w := bucket.Object("negative").NewWriter(newTestCtx(t))
			w.ChunkSize = -1
			w.UseFileBuffer = true
			w.FileBufferDir = t.TempDir()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("negative ChunkSize panicked: %v", r)
				}
			}()
			defer func() { _ = w.Close() }()
			if err := tc.write(w); err == nil || !strings.Contains(err.Error(), "ChunkSize") {
				t.Errorf("operation error = %v, want ChunkSize validation error", err)
			}
			if err := w.Close(); err == nil || !strings.Contains(err.Error(), "ChunkSize") {
				t.Errorf("Close error = %v, want ChunkSize validation error", err)
			}
			for _, call := range []string{"b2_start_large_file", "upload_file", "upload_part", "b2_finish_large_file"} {
				if got := len(srv.Calls(call)); got != 0 {
					t.Errorf("%s calls = %d, want 0", call, got)
				}
			}
			if entries, err := os.ReadDir(w.FileBufferDir); err != nil || len(entries) != 0 {
				t.Errorf("buffer directory = %v, %v; want no files", entries, err)
			}
		})
	}
}

type readerErrorWriteBuffer struct {
	writeBuffer
	err error
}

func (b readerErrorWriteBuffer) Reader() (readResetter, error) { return nil, b.err }

func TestWriterChunkReaderFailureRemovesTempFile(t *testing.T) {
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
	w := bucket.Object("reader-failure").NewWriter(newTestCtx(t))
	w.ChunkSize = 10
	dir := t.TempDir()
	want := errors.New("buffer reader failed")
	w.newBuffer = func() (writeBuffer, error) {
		buffer, err := newFileBuffer(dir)
		if err != nil {
			return nil, err
		}
		return readerErrorWriteBuffer{buffer, want}, nil
	}
	_, _ = w.Write(bytes.Repeat([]byte("a"), 25))
	if err := w.Close(); !errors.Is(err, want) {
		t.Fatalf("Close error = %v, want buffer reader failed", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("buffer directory = %v, %v; want no files", entries, err)
	}
	if got := len(srv.Calls("b2_finish_large_file")); got != 0 {
		t.Errorf("finish calls = %d, want 0", got)
	}
}
