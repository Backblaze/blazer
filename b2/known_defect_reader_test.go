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
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// The download chunk offset must be computed in 64-bit arithmetic:
// int64(chunkID*csize) multiplies in int first, which overflows on 32-bit
// platforms for objects over about 2 GiB. The defect exists only where int is
// 32 bits, so the test skips elsewhere; run it with
//
//	make known-defects-32bit
//
// It does not download anything: the reader is positioned at chunk 3 of
// 1 GiB chunks and the fake server answers the ranged request with 416, so
// only the requested Range header is observed (3 * 1 GiB = 3221225472).
func TestKnownDefect_ChunkOffsetMultipliedIn64Bit(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skipf("int is %d bits; the defect only exists on 32-bit platforms (make known-defects-32bit)", strconv.IntSize)
	}
	srv, bucket := newSHA1WireBucket(t)
	srv.Handle("big", func(r b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.Response{Status: http.StatusRequestedRangeNotSatisfiable, Body: `{"status":416,"code":"range_not_satisfiable","message":"past the end"}`}
	})
	reader := bucket.Object("big").NewReader(newTestCtx(t))
	reader.ChunkSize = 1 << 30
	reader.ConcurrentDownloads = 1
	reader.chwid, reader.chrid = 3, 3
	defer reader.Close()
	if _, err := reader.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("Read past the end returned %v, want io.EOF", err)
	}
	calls := srv.Calls("big")
	if len(calls) != 1 {
		t.Fatalf("download requests = %d, want 1", len(calls))
	}
	const want = "bytes=3221225472-4294967295"
	if got := calls[0].Header.Get("Range"); got != want {
		t.Errorf("Range of the chunk-3 request = %q, want %q (chunk offset overflowed int)", got, want)
	}
}
