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
	if closeErr != nil {
		if copyErr == nil || !errors.Is(closeErr, copyErr) {
			t.Fatalf("Close = %v; want the source error from io.Copy = %v", closeErr, copyErr)
		}
		if uploads := srv.Calls("upload_file"); len(uploads) != 0 {
			t.Fatalf("failed copy made %d uploads; want none", len(uploads))
		}
		return
	}
	uploads := srv.Calls("upload_file")
	if len(uploads) != 1 {
		t.Fatalf("Close succeeded with %d uploads; want one upload containing %q", len(uploads), payload)
	}
	if !bytes.Equal(uploads[0].Body, payload) {
		t.Fatalf("Close succeeded with uploaded body %q after io.Copy = (%d, %v); want %q", uploads[0].Body, n, copyErr, payload)
	}
}
