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
	}{
		{name: "empty"},
		{name: "small", payload: "ab"},
		{name: "large", payload: "abcdefg"},
		{name: "failed close", payload: "ab", failClose: true},
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
			if tc.failClose {
				srv.Reply("b2_get_upload_url", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"upload unavailable"}`)
			}
			writer := bucket.Object("object").NewWriter(context.Background())
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
