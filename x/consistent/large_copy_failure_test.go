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

package consistent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

type cleanupTransport struct {
	sim        http.RoundTripper
	starts     atomic.Int32
	failCancel bool
}

func (tr *cleanupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/b2_start_large_file") {
		tr.starts.Add(1)
	}
	if tr.failCancel && strings.HasSuffix(req.URL.Path, "/b2_cancel_large_file") {
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":400,"code":"bad_request","message":"cleanup rejected"}`)), Request: req}, nil
	}
	return tr.sim.RoundTrip(req)
}

type largeFailingWriterTo struct{ *failingSourceReader }

func (f largeFailingWriterTo) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.data)
	f.data = f.data[n:]
	if err != nil {
		return int64(n), err
	}
	return int64(n), f.err
}

func TestOperateStreamCancelsUnfinishedLargeUpload(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		writerTo, failCancel bool
	}{
		{"reader", false, false}, {"writer_to", true, false}, {"cleanup_error", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			transport := &cleanupTransport{sim: b2sim.New(), failCancel: tc.failCancel}
			client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(transport), b2.DefaultWriterOptions(b2.WriterOptionFunc(func(w *b2.Writer) { w.ChunkSize = 5_000_000 })))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.NewBucket(ctx, "consistent", nil)
			if err != nil {
				t.Fatal(err)
			}
			srcErr := errors.New("callback output failed")
			src := &failingSourceReader{data: bytes.Repeat([]byte("x"), 5_000_001), err: srcErr}
			err = NewGroup(bucket, "consistent").OperateStream(ctx, "obj", func(io.Reader) (io.Reader, error) {
				if tc.writerTo {
					return largeFailingWriterTo{src}, nil
				}
				return src, nil
			})
			if !errors.Is(err, srcErr) {
				t.Fatalf("OperateStream err = %v, want source error", err)
			}
			if tc.failCancel && !strings.Contains(err.Error(), "cleanup rejected") {
				t.Errorf("OperateStream err = %v, want cleanup failure", err)
			}
			if transport.starts.Load() != 1 {
				t.Fatalf("large uploads started = %d, want 1", transport.starts.Load())
			}
			if n := len(client.Status().Writers); n != 0 {
				t.Errorf("%d writers remain open", n)
			}
			// The simulator rejects deleting a bucket with an unfinished upload.
			deleteErr := bucket.Delete(ctx)
			if tc.failCancel {
				if deleteErr == nil || !strings.Contains(deleteErr.Error(), "bucket has unfinished large files") {
					t.Errorf("bucket deletion = %v, want unfinished-upload rejection", deleteErr)
				}
			} else if deleteErr != nil {
				t.Errorf("bucket deletion after cleanup: %v", deleteErr)
			}
		})
	}
}
