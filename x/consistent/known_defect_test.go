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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
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

// nullInfoTransport serves every call from the in-process simulator but
// reports each bucket's info map as absent (bucketInfo: null) when buckets
// are listed, the way a bucket created without any info is sometimes listed.
type nullInfoTransport struct{ sim http.RoundTripper }

func (n nullInfoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := n.sim.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/b2_list_buckets") {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var listed struct {
		Buckets []map[string]any `json:"buckets"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		return nil, err
	}
	for _, b := range listed.Buckets {
		b["bucketInfo"] = nil
	}
	if body, err = json.Marshal(listed); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// A bucket whose info map is absent must not crash Operate: it must run the
// callback, store its result and return nil. The whole operation is served by
// the simulator, so no step of it is left unscripted.
func TestKnownDefect_BucketWithoutInfoMapDoesNotCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sim := b2sim.New()
	setup, err := b2.NewClient(ctx, "a", "k", b2.Transport(sim))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.NewBucket(ctx, "n", nil); err != nil {
		t.Fatal(err)
	}
	client, err := b2.NewClient(ctx, "a", "k", b2.Transport(nullInfoTransport{sim: sim}))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Info != nil {
		t.Fatalf("setup: the listed bucket has info %v, want an absent (nil) info map", attrs.Info)
	}
	var (
		ran    bool
		opErr  error
		panics any
	)
	func() {
		defer func() { panics = recover() }()
		opErr = NewGroup(bucket, "g").Operate(ctx, "obj", func(b []byte) ([]byte, error) {
			ran = true
			return []byte(fmt.Sprint(len(b))), nil
		})
	}()
	if panics != nil {
		t.Fatalf("Operate panicked on a bucket with no info map: %v", panics)
	}
	if opErr != nil {
		t.Fatalf("Operate on a bucket with no info map returned %v, want nil", opErr)
	}
	if !ran {
		t.Error("the Operate callback never ran")
	}
	// The stored state must hold the callback's output ("0": it saw no data).
	// Read it through an independent client and group over the same simulator.
	b2bucket, err := setup.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	err = NewGroup(b2bucket, "g").Operate(ctx, "obj", func(b []byte) ([]byte, error) {
		stored = append([]byte(nil), b...)
		return b, nil
	})
	if err != nil {
		t.Fatalf("reading back the stored object: %v", err)
	}
	if string(stored) != "0" {
		t.Errorf("stored object = %q, want %q", stored, "0")
	}
}
