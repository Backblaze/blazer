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
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFindingB08_PipeSourceDoesNotProduceEmptyObject(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	source, feed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("OS pipe bytes must not become an empty object")
	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		defer func() { _ = feed.Close() }()
		_, _ = feed.Write(payload)
	}()
	t.Cleanup(func() {
		_ = source.Close()
		_ = feed.Close()
		<-feedDone
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	writer := bucket.Object("pipe-source").NewWriter(ctx)
	n, copyErr := io.Copy(writer, source)
	closeErr := writer.Close()
	t.Logf("io.Copy = (%d, %v); Close = %v", n, copyErr, closeErr)
	if copyErr != nil || n != int64(len(payload)) {
		t.Errorf("io.Copy = (%d, %v); want (%d, nil)", n, copyErr, len(payload))
	}
	if closeErr != nil {
		t.Fatalf("Close = %v; want nil after copying the pipe", closeErr)
	}
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 {
		t.Fatalf("Close succeeded with %d uploads; want one upload containing %q", len(uploads), payload)
	}
	if !bytes.Equal(uploads[0].Body, payload) {
		t.Fatalf("Close succeeded with uploaded body %q after io.Copy = (%d, %v); want %q", uploads[0].Body, n, copyErr, payload)
	}
}

func TestWriterWriteAfterClose(t *testing.T) {
	for _, tc := range []struct {
		name      string
		payload   string
		failClose bool
		failLarge bool
	}{
		{name: "empty"},
		{name: "small", payload: "ab"},
		{name: "large", payload: "abcdefg"},
		{name: "failed close", payload: "ab", failClose: true},
		{name: "failed large close", payload: "abcdefg", failClose: true, failLarge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Write after Close panicked: %v", r)
				}
			}()
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"object","action":"upload"}`)
			if tc.failLarge {
				srv.Reply("b2_finish_large_file", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"finish failed"}`)
			} else if tc.failClose {
				srv.Reply("b2_get_upload_url", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"upload unavailable"}`)
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := bucket.Object("object").NewWriter(parent)
			// The recording server accepts small parts to exercise multipart locally.
			writer.ChunkSize = 3
			writer.ConcurrentUploads = 1
			if tc.payload != "" {
				if n, err := writer.Write([]byte(tc.payload)); err != nil || n != len(tc.payload) {
					t.Fatalf("initial Write = (%d, %v), want (%d, nil)", n, err, len(tc.payload))
				}
			}
			closeErr := writer.Close()
			if (closeErr != nil) != tc.failClose {
				t.Fatalf("Close = %v, want error: %t", closeErr, tc.failClose)
			}
			if !errors.Is(writer.ctx.Err(), context.Canceled) {
				t.Errorf("writer context after Close = %v, want canceled", writer.ctx.Err())
			}
			if parent.Err() != nil {
				t.Errorf("parent context after Close = %v, want nil", parent.Err())
			}
			if tc.failClose && !errors.Is(context.Cause(writer.ctx), closeErr) {
				t.Errorf("writer context cause = %v, want Close error %v", context.Cause(writer.ctx), closeErr)
			}
			if tc.failClose && !tc.failLarge && !strings.Contains(closeErr.Error(), "upload unavailable") {
				t.Errorf("Close = %v, want upload unavailable", closeErr)
			}
			if tc.failLarge && (!strings.Contains(closeErr.Error(), "finish failed") || len(srv.Calls("b2_finish_large_file")) != 1) {
				t.Fatalf("large Close = %v, want the finish failure", closeErr)
			}
			if tc.name == "large" && len(srv.Calls("b2_finish_large_file")) != 1 {
				t.Fatal("large upload was not finished")
			}
			before := len(srv.Requests())
			if n, err := writer.Write([]byte("late")); n != 0 || !errors.Is(err, ErrClosed) {
				t.Errorf("Write after Close = (%d, %v), want (0, ErrClosed)", n, err)
			}
			if got := len(srv.Requests()); got != before {
				t.Errorf("requests after Write = %d, want %d", got, before)
			}
		})
	}
}

func TestWriterReadFromAfterClose(t *testing.T) {
	for _, tc := range []struct {
		name     string
		buffered bool
	}{
		{name: "seekable"},
		{name: "buffered", buffered: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			writer := bucket.Object("closed-reader").NewWriter(context.Background())
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			before := len(srv.Requests())
			input := bytes.NewReader([]byte("x"))
			var source io.Reader = input
			if tc.buffered {
				source = struct{ io.Reader }{input}
			}
			if n, err := writer.ReadFrom(source); n != 0 || !errors.Is(err, ErrClosed) {
				t.Errorf("ReadFrom after Close = (%d, %v), want (0, ErrClosed)", n, err)
			}
			if input.Len() != 1 {
				t.Errorf("input remaining = %d, want 1", input.Len())
			}
			if got := len(srv.Requests()); got != before {
				t.Errorf("requests after ReadFrom = %d, want %d", got, before)
			}
		})
	}
}

func TestReadFromPositionedFile(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"positioned","action":"upload"}`)
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close source: %v", err)
		}
	}()
	if _, err := f.WriteString("HEADER--payload"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	writer := bucket.Object("positioned").NewWriter(context.Background())
	// Seven bytes remain: less than this chunk, while the full file is larger.
	// This subminimum chunk size is only suitable for the recording server.
	writer.ChunkSize = 10
	n, err := io.Copy(writer, f)
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n != int64(len("payload")) {
		t.Errorf("io.Copy = %d, want 7", n)
	}
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(uploads))
	}
	if starts, parts := len(srv.Calls("b2_start_large_file")), len(srv.Calls("upload_part")); starts != 0 || parts != 0 {
		t.Errorf("multipart starts = %d, parts = %d; want neither", starts, parts)
	}
	body := uploads[0].Body
	if uploads[0].Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" && len(body) >= 40 {
		body = body[:len(body)-40]
	}
	if string(body) != "payload" {
		t.Errorf("uploaded = %q, want payload", body)
	}
}

type endSeekFailure struct{ *bytes.Reader }

func (r endSeekFailure) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		return 0, errors.New("end seek unsupported")
	}
	return r.Reader.Seek(offset, whence)
}

func TestReadFromFallsBackWhenEndSeekFails(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	source := endSeekFailure{bytes.NewReader([]byte("HEADER--payload"))}
	if _, err := source.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("end-seek-failure").NewWriter(context.Background())
	if n, err := w.ReadFrom(source); err != nil || n != 7 {
		t.Fatalf("ReadFrom = (%d, %v), want (7, nil)", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 || string(uploads[0].Body) != "payload" {
		t.Fatalf("uploads = %v, want one upload of payload", uploads)
	}
}

func TestReadFromPositionedFileMultipart(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"object","action":"upload"}`)
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close source: %v", err)
		}
	}()
	if _, err := f.WriteString("HEADER--payload"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(8, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("object").NewWriter(context.Background())
	// The recording server accepts small parts to exercise multipart locally.
	w.ChunkSize = 3
	w.ConcurrentUploads = 1
	if n, err := io.Copy(w, f); err != nil || n != 7 {
		t.Fatalf("io.Copy = (%d, %v), want (7, nil)", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"1": "pay", "2": "loa", "3": "d"}
	parts := srv.Calls("upload_part")
	if len(parts) != len(want) {
		t.Fatalf("parts = %d, want %d", len(parts), len(want))
	}
	for _, part := range parts {
		number := part.Header.Get("X-Bz-Part-Number")
		body := part.Body
		if part.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" && len(body) >= 40 {
			body = body[:len(body)-40]
		}
		if expected, ok := want[number]; !ok || string(body) != expected {
			t.Errorf("part %s = %q, want %q", number, body, expected)
		}
		delete(want, number)
	}
	if len(want) != 0 {
		t.Errorf("missing parts: %v", want)
	}
}

func TestReadFromPastEndUploadsEmpty(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	source := bytes.NewReader([]byte("abc"))
	if _, err := source.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("past-end").NewWriter(context.Background())
	if n, err := w.ReadFrom(source); err != nil || n != 0 {
		t.Fatalf("ReadFrom = (%d, %v), want (0, nil)", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 || len(uploads[0].Body) != 0 {
		t.Fatalf("uploads = %v, want one empty upload", uploads)
	}
}

func TestEmptyWriterClosesFileBufferAndUnregisters(t *testing.T) {
	_, bucket := newSHA1WireBucket(t)
	dir := t.TempDir()
	w := bucket.Object("empty-buffer").NewWriter(context.Background())
	w.UseFileBuffer = true
	w.FileBufferDir = dir
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temporary buffers remaining = %d, want 0", len(entries))
	}
	if writers := bucket.c.Status().Writers; len(writers) != 0 {
		t.Errorf("registered writers = %v, want none", writers)
	}
}
