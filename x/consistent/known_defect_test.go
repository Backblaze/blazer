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

//go:build knowndefect

package consistent

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/internal/b2wiretest"
	"github.com/Backblaze/blazer/x/b2sim"
)

func newSimGroup(t *testing.T) (*b2.Client, *Group, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(b2sim.New()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "consistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	return client, NewGroup(bucket, "consistent"), ctx
}

type failingSourceReader struct {
	data []byte
	err  error
}

func (f *failingSourceReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// When copying the callback's output fails, OperateStream must
// close its writer; otherwise the upload stays registered (and, for a large
// file, unfinished on the server).
func TestKnownDefect_OperateStreamClosesWriterWhenCopyFails(t *testing.T) {
	client, g, ctx := newSimGroup(t)
	srcErr := errors.New("callback output failed")
	err := g.OperateStream(ctx, "obj", func(io.Reader) (io.Reader, error) {
		return &failingSourceReader{data: []byte("abc"), err: srcErr}, nil
	})
	if !errors.Is(err, srcErr) {
		t.Fatalf("OperateStream err = %v, want %v", err, srcErr)
	}
	if n := len(client.Status().Writers); n != 0 {
		t.Errorf("%d writer(s) left open after OperateStream failed to copy", n)
	}
}

// A callback that returns its input reader unchanged must work.
func TestKnownDefect_OperateStreamAcceptsCallbackReturningItsInput(t *testing.T) {
	_, g, ctx := newSimGroup(t)
	if err := g.Operate(ctx, "obj", func([]byte) ([]byte, error) { return []byte("hello"), nil }); err != nil {
		t.Fatal(err)
	}
	err := g.OperateStream(ctx, "obj", func(r io.Reader) (io.Reader, error) { return r, nil })
	if err != nil {
		t.Errorf("OperateStream with an identity callback failed: %v", err)
	}
	var got []byte
	if err := g.Operate(ctx, "obj", func(b []byte) ([]byte, error) { got = append(got[:0], b...); return b, nil }); err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content after the identity callback = %q, want %q", got, "hello")
	}
}

// A bucket whose info map is absent must not crash Operate.
func TestKnownDefect_BucketWithoutInfoMapDoesNotCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":null,"lifecycleRules":[],"revision":1,`+
		`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false}}}]}`)
	client, err := b2.NewClient(ctx, "a", "k", b2.APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Operate panicked on a bucket with no info map: %v", r)
		}
	}()
	_ = NewGroup(bucket, "g").Operate(ctx, "obj", func(b []byte) ([]byte, error) { return []byte(fmt.Sprint(len(b))), nil })
}
