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
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2simtest"
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
			sim := b2simtest.New(t)
			transport := &cleanupTransport{sim: sim.Transport(), failCancel: tc.failCancel}
			client, err := b2.NewClient(ctx, b2simtest.KeyID, b2simtest.Key, b2.APIBase(sim.URL), b2.Transport(transport), b2.DefaultWriterOptions(b2.WriterOptionFunc(func(w *b2.Writer) { w.ChunkSize = 5_000_000 })))
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
			if tc.failCancel {
				cleanupErr := findCleanupError(err)
				if cleanupErr == nil {
					t.Errorf("OperateStream err = %v, want matchable B2 cleanup failure", err)
				} else {
					if !errors.Is(err, cleanupErr) {
						t.Errorf("errors.Is(%v, cleanup error) = false", err)
					}
					target := reflect.New(reflect.TypeOf(cleanupErr)).Interface()
					if !errors.As(err, target) {
						t.Errorf("errors.As(%v, cleanup error type) = false", err)
					}
				}
			}
			if transport.starts.Load() != 1 {
				t.Fatalf("large uploads started = %d, want 1", transport.starts.Load())
			}
			if n := len(client.Status().Writers); n != 0 {
				t.Errorf("%d writers remain open", n)
			}
			// B2 refuses to delete a bucket that still holds an unfinished large file:
			// 400 cannot_delete_non_empty_bucket (https://www.backblaze.com/apidocs/b2-delete-bucket).
			deleteErr := bucket.Delete(ctx)
			if tc.failCancel {
				if status, code, _ := base.MsgCode(deleteErr); status != 400 || code != "cannot_delete_non_empty_bucket" {
					t.Errorf("bucket deletion = %v, want 400 cannot_delete_non_empty_bucket for the unfinished upload", deleteErr)
				}
			} else if deleteErr != nil {
				t.Errorf("bucket deletion after cleanup: %v", deleteErr)
			}
		})
	}
}

// findCleanupError locates the simulator's specific B2 error in an error tree.
func findCleanupError(err error) error {
	if err == nil {
		return nil
	}
	status, code, message := base.MsgCode(err)
	if status == 400 && code == "bad_request" && message == "cleanup rejected" {
		return err
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range wrapped.Unwrap() {
			if match := findCleanupError(child); match != nil {
				return match
			}
		}
	case interface{ Unwrap() error }:
		return findCleanupError(wrapped.Unwrap())
	}
	return nil
}
