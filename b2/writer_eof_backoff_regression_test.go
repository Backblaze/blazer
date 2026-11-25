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
	"testing"
	"time"
)

// After a failed upload attempt the retry must wait (backoff),
// not retry immediately.
func TestRegression_UploadRetryWaitsBetweenAttempts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		op    string
		chunk int
		size  int64
	}{
		{"simple upload", "uploadFile", 0, 1e4},
		{"part upload", "uploadPart", 1e4, 3e4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timer := newTestAfter()
			root := &testRoot{
				afterFunc: timer.wait,
				bucketMap: make(map[string]map[string]string),
				errs: &errCont{errMap: map[string]map[int]error{
					tc.op: {0: testError{reupload: true, maxReuploads: 3}, 1: testError{reupload: true, maxReuploads: 3}},
				}},
			}
			client := &Client{backend: &beRoot{b2i: root}}
			b, err := client.NewBucket(newTestCtx(t), "fun", &BucketAttrs{Type: Private})
			if err != nil {
				t.Fatal(err)
			}
			w := b.Object("foo").NewWriter(newTestCtx(t))
			w.ChunkSize = tc.chunk
			if _, err := io.Copy(w, io.LimitReader(zReader{}, tc.size)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			delays := timer.durations()
			if len(delays) != 2 {
				t.Fatalf("%d retry waits recorded, want 2 (one per retry)", len(delays))
			}
			if delays[0] < time.Second || delays[0] > 1050*time.Millisecond {
				t.Errorf("first retry waited %v, want about one second", delays[0])
			}
			if delays[1] <= delays[0] {
				t.Errorf("retry delays %v did not grow", delays)
			}
		})
	}
}

// A part upload that fails with a bare io.EOF must surface as an
// error from Write/Close, not stall or silently succeed.
func TestRegression_BareEOFFromPartUploadIsReported(t *testing.T) {
	root := &testRoot{
		bucketMap: make(map[string]map[string]string),
		errs: &errCont{errMap: map[string]map[int]error{
			"uploadPart": {0: io.EOF},
		}},
	}
	client := &Client{backend: &beRoot{b2i: root}}
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	b, err := client.NewBucket(parent, "fun", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	w := b.Object("foo").NewWriter(parent)
	w.ChunkSize = 1e4
	type result struct{ werr, cerr error }
	done := make(chan result, 1)
	go func() {
		_, werr := io.Copy(w, io.LimitReader(zReader{}, 5e4))
		done <- result{werr, w.Close()}
	}()
	select {
	case r := <-done:
		if !errors.Is(r.werr, io.ErrUnexpectedEOF) && !errors.Is(r.cerr, io.ErrUnexpectedEOF) {
			t.Errorf("Write error = %v, Close error = %v; want io.ErrUnexpectedEOF", r.werr, r.cerr)
		}
	case <-time.After(5 * time.Second):
		cancelParent() // release the stuck goroutine
		<-done
		t.Error("Write/Close blocked after a part upload failed with io.EOF")
	}
}

type eofPartURL struct{ beLargeFileInterface }

func (eofPartURL) getUploadPartURL(context.Context) (beFileChunkInterface, error) {
	return nil, io.EOF
}

func TestRegression_BareEOFFromPartURLIsReported(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	w := &Writer{ctx: ctx, cancel: cancel, file: eofPartURL{}}
	w.thread()
	w.wg.Wait()
	if err := w.getErr(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("worker error = %v, want io.ErrUnexpectedEOF", err)
	}
	if !errors.Is(context.Cause(ctx), io.ErrUnexpectedEOF) {
		t.Errorf("worker context cause = %v, want io.ErrUnexpectedEOF", context.Cause(ctx))
	}
}
