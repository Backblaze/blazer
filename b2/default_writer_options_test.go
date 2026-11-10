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
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
	"github.com/Backblaze/blazer/x/b2sim"
)

func TestDefaultWriterOptionsRunOnlyForRealWriters(t *testing.T) {
	ctx := context.Background()
	calls := 0
	client, err := NewClient(ctx, "offline-id", "offline-key", Transport(b2sim.New()),
		DefaultWriterOptions(func(w *Writer) {
			calls++
			if w.o == nil || w.ctx == nil || w.cancel == nil {
				t.Error("default option received an uninitialized writer")
			}
			w.ChunkSize = 5_000_000
		}, WithAttrsOption(&Attrs{ContentType: "text/default"})))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("option calls after NewClient = %d, want 0", calls)
	}
	bucket, err := client.NewBucket(ctx, "writer-options", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		opts        []WriterOption
		wantCalls   int
		contentType string
	}{
		{"default", nil, 1, "text/default"},
		{"override", []WriterOption{WithAttrsOption(&Attrs{ContentType: "text/override"})}, 2, "text/override"},
	} {
		object := bucket.Object(tc.name)
		writer := object.NewWriter(ctx, tc.opts...)
		if calls != tc.wantCalls {
			t.Errorf("option calls after %s NewWriter = %d, want %d", tc.name, calls, tc.wantCalls)
		}
		if writer.ChunkSize != 5_000_000 {
			t.Errorf("%s ChunkSize = %d, want 5000000", tc.name, writer.ChunkSize)
		}
		_, writeErr := writer.Write([]byte("payload"))
		closeErr := writer.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("%s upload: Write = %v, Close = %v", tc.name, writeErr, closeErr)
		}
		attrs, err := object.Attrs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if attrs.ContentType != tc.contentType {
			t.Errorf("%s ContentType = %q, want %q", tc.name, attrs.ContentType, tc.contentType)
		}
	}
}

func TestDefaultWriterOptionsDoNotRunWhenAuthorizationFails(t *testing.T) {
	server := b2wiretest.New(t)
	server.StrictReplySequence("b2_authorize_account", b2wiretest.Response{
		Status: 400,
		Body:   `{"status":400,"code":"bad_request","message":"authorization rejected"}`,
	})
	calls := 0
	client, err := NewClient(context.Background(), "offline-id", "offline-key", APIBase(server.URL()),
		DefaultWriterOptions(func(w *Writer) { calls++ }))
	if client != nil {
		t.Error("failed authorization returned a client")
	}
	if status, message := base.Code(err); status != 400 || message != "authorization rejected" {
		t.Fatalf("authorization error = %v (%d, %q), want 400 authorization rejected", err, status, message)
	}
	if got := len(server.Calls("b2_authorize_account")); got != 1 {
		t.Errorf("authorization requests = %d, want 1", got)
	}
	if calls != 0 {
		t.Errorf("option calls after failed authorization = %d, want 0", calls)
	}
}
