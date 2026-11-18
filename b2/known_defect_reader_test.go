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

package b2

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
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Reader.Verify before any Read (or after a failed key check)
// must return (nil, false), not panic.
func TestKnownDefect_VerifyBeforeReadDoesNotPanic(t *testing.T) {
	_, bucket := newSHA1WireBucket(t)
	for _, tc := range []struct {
		name string
		mk   func() *Reader
	}{
		{"before any Read", func() *Reader { return bucket.Object("o").NewReader(newTestCtx(t)) }},
		{"after a rejected SSE-C key", func() *Reader {
			bad := &SSECKey{} // a zero key is rejected by validation
			r := bucket.Object("o").NewReader(newTestCtx(t), bad)
			_, _ = r.Read(make([]byte, 1))
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.mk()
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Verify panicked: %v", p)
				}
			}()
			if err, ok := r.Verify(); err != nil || ok {
				t.Errorf("Verify = (%v, %v), want (nil, false)", err, ok)
			}
		})
	}
}

// Reader status progress is indexed by chunk id; chunk ids start
// at 0, so the first chunk must not be dropped and an active chunk must not
// read as 100%.
func TestKnownDefect_ReaderStatusIndexesChunksFromZero(t *testing.T) {
	half := &meteredReader{size: 10}
	half.read.Store(5)
	r := &Reader{smap: map[int]*meteredReader{0: nil, 1: half}} // chunk 0 finished, chunk 1 half done
	got := r.status().Progress
	if len(got) != 2 || got[0] != 1 || got[1] != 0.5 {
		t.Errorf("Progress = %v, want [1 0.5] (chunk 0 done, chunk 1 half done)", got)
	}
	single := &Reader{smap: map[int]*meteredReader{0: half}}
	if got := single.status().Progress; len(got) != 1 || got[0] != 0.5 {
		t.Errorf("one active download at 50%% reports %v, want [0.5]", got)
	}
}

// All chunks of one download must come from one object version;
// an overwrite during a long read must not produce a mix of two versions.
func TestKnownDefect_OverwriteDuringReadDoesNotMixVersions(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	var mu sync.Mutex
	gets := 0
	serve := func(body, id string, r b2wiretest.Request) b2wiretest.Response {
		resp := b2wiretest.RangeDownload(body, r.Header.Get("Range"))
		if resp.Header == nil {
			resp.Header = http.Header{}
		}
		resp.Header.Set("X-Bz-File-Id", id)
		return resp
	}
	srv.Handle("ovw", func(r b2wiretest.Request) b2wiretest.Response {
		if r.Method == http.MethodHead {
			return b2wiretest.Response{Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {"idA"}}}
		}
		mu.Lock()
		gets++
		n := gets
		mu.Unlock()
		if n > 1 { // the object was overwritten after the first chunk
			return serve("BBBBBBBB", "idB", r)
		}
		return serve("AAAAAAAA", "idA", r)
	})
	srv.Handle("b2_download_file_by_id", func(r b2wiretest.Request) b2wiretest.Response {
		if r.Query.Get("fileId") == "idA" {
			return serve("AAAAAAAA", "idA", r)
		}
		return serve("BBBBBBBB", "idB", r)
	})
	reader := bucket.Object("ovw").NewReader(newTestCtx(t))
	reader.ChunkSize = 4
	reader.ConcurrentDownloads = 1
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err == nil && string(got) != "AAAAAAAA" {
		t.Errorf("read %q after an overwrite mid-download, want the first version \"AAAAAAAA\" or an error", got)
	}
}

// The
// download chunk offset must be computed in 64-bit arithmetic. int64(a*b)
// multiplies in int first, which overflows on 32-bit platforms for objects
// over about 2 GiB.
func TestKnownDefect_ChunkOffsetMultipliedIn64Bit(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "reader.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "int64" {
			return true
		}
		if bin, ok := call.Args[0].(*ast.BinaryExpr); ok && bin.Op == token.MUL {
			t.Errorf("reader.go:%d multiplies in int before widening to int64 (overflows on 32-bit); convert each operand to int64 first", fset.Position(call.Pos()).Line)
		}
		return true
	})
}
