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
	"bytes"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	for _, tc := range []struct {
		name      string
		overwrite bool
	}{{"unchanged control", false}, {"overwritten", true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			var mu sync.Mutex
			gets := 0
			var versions, requestedIDs []string
			serve := func(body, id string, r b2wiretest.Request) b2wiretest.Response {
				mu.Lock()
				versions = append(versions, id)
				requestedIDs = append(requestedIDs, r.Query.Get("fileId"))
				mu.Unlock()
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
				if tc.overwrite && n > 1 {
					return serve("BBBBBBBB", "idB", r)
				}
				return serve("AAAAAAAA", "idA", r)
			})
			srv.Handle("b2_download_file_by_id", func(r b2wiretest.Request) b2wiretest.Response {
				switch r.Query.Get("fileId") {
				case "idA":
					return serve("AAAAAAAA", "idA", r)
				case "idB":
					return serve("BBBBBBBB", "idB", r)
				default:
					return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"unexpected fileId"}`}
				}
			})
			ctx := newTestCtx(t)
			reader := bucket.Object("ovw").NewReader(ctx)
			reader.ChunkSize = 4
			reader.ConcurrentDownloads = 1
			got, err := io.ReadAll(reader)
			_ = reader.Close()
			if ctx.Err() != nil {
				t.Fatalf("read exceeded its context: %v", ctx.Err())
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !tc.overwrite || !changedVersionError(err) {
					t.Fatalf("read returned an unrelated error: %v", err)
				}
				if !bytes.HasPrefix([]byte("AAAAAAAA"), got) {
					t.Errorf("bytes before the version-change error = %q, want only a prefix of version A", got)
				}
				if !strings.Contains(strings.Join(versions, ","), "idB") {
					t.Errorf("version-change error without observing version B: replies=%v", versions)
				}
				return
			}
			if string(got) != "AAAAAAAA" {
				t.Errorf("read %q, want the first version %q", got, "AAAAAAAA")
			}
			if len(requestedIDs) < 2 {
				t.Errorf("download requests = %d, want at least two chunks", len(requestedIDs))
			}
			if tc.overwrite {
				for i, id := range requestedIDs {
					if i > 0 && id != "idA" {
						t.Errorf("chunk request %d used fileId=%q, want idA", i, id)
					}
				}
			}
		})
	}
}

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

// The goroutine that waits for a chunk must not stay blocked after Close.
// When the download worker sees the cancelled context before it takes its
// first buffer, it returns without waking the goroutine that (*Reader).curChunk
// parked in the condition wait, and that goroutine stays blocked. Moved here
// from the regular suite because it fails intermittently on the current code
// (roughly once per few thousand closed readers, so a pass proves nothing).
func TestKnownDefect_NoChunkWaiterLeftBlockedAfterClose(t *testing.T) {
	const readers = 1000
	bucket := blockingDownload(t)
	before := goroutinesIn("(*Reader).curChunk")
	for i := 0; i < readers; i++ {
		r := newSlowReader(bucket, newTestCtx(t))
		done := make(chan any, 1)
		parked := chunkWaitersParked()
		go func() { done <- recoverRead(r) }()
		if !awaitCond(func() bool { return newChunkWaiterParked(parked) }) {
			closeWithin(r, 3*time.Second)
			t.Fatalf("reader %d: the Read never parked waiting for a chunk", i)
		}
		if !closeWithin(r, 3*time.Second) {
			t.Fatalf("reader %d: Close did not return", i)
		}
		select {
		case p := <-done:
			if p != nil {
				t.Fatalf("reader %d: Read panicked: %v", i, p)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("reader %d: Read did not return after Close", i)
		}
	}
	var now int
	if !awaitCond(func() bool { now = goroutinesIn("(*Reader).curChunk"); return now <= before }) {
		t.Errorf("%d chunk-waiter goroutine(s) still blocked after %d closed readers (before: %d)", now-before, readers, before)
	}
}

func goroutinesIn(fn string) int {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), fn)
}
