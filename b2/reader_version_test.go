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
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestReaderHEADDoesNotReplacePinnedVersion(t *testing.T) {
	for _, headID := range []string{"", "new-id"} {
		t.Run(fmt.Sprintf("HEAD ID=%q", headID), func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Handle("versioned", func(req b2wiretest.Request) b2wiretest.Response {
				response := b2wiretest.RangeDownload("newbytes", req.Header.Get("Range"))
				response.Header.Set("X-Bz-File-Id", "new-id")
				return response
			})
			srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
				if req.Method == http.MethodHead {
					return b2wiretest.Response{Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {headID}}}
				}
				body := "oldbytes"
				if req.Query.Get("fileId") != "old-id" {
					body = "newbytes"
				}
				response := b2wiretest.RangeDownload(body, req.Header.Get("Range"))
				response.Header.Set("X-Bz-File-Id", req.Query.Get("fileId"))
				return response
			})
			object := bucket.Object("versioned")
			object.f = bucket.b.file("old-id", "versioned")
			reader := object.NewRangeReader(newTestCtx(t), 1, 6)
			reader.ChunkSize = 3
			var samples []progressSample
			reader.Progress = func(n, total int64) { samples = append(samples, progressSample{n, total}) }
			t.Cleanup(func() { _ = reader.Close() })
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != "ldbyte" {
				t.Fatalf("historical range after HEAD = %q, %v; want ldbyte, nil", got, err)
			}
			if n := len(srv.Calls("versioned")); n != 0 {
				t.Errorf("name downloads = %d, want 0 for the pinned version", n)
			}
			for _, req := range srv.Calls("b2_download_file_by_id") {
				if id := req.Query.Get("fileId"); id != "old-id" {
					t.Errorf("download ID = %q, want old-id", id)
				}
			}
			assertProgressSamples(t, samples, 6, 6, false)
		})
	}
}

func TestReaderStatusPreservesSparseChunkIDs(t *testing.T) {
	half := &meteredReader{size: 10}
	half.read.Store(5)
	for _, tc := range []struct {
		name       string
		mapEntries map[int]*meteredReader
		want       []float64
	}{
		{"first response delayed", map[int]*meteredReader{1: half}, []float64{0, 0.5}},
		{"middle response delayed", map[int]*meteredReader{0: nil, 2: half}, []float64{1, 0, 0.5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &Reader{smap: tc.mapEntries}
			if got := reader.status().Progress; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sparse chunk progress = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReaderStatusIncludesAllocatedChunks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader *Reader
		want   []float64
	}{
		{"waiting for first headers", &Reader{chwid: 1}, []float64{0}},
		{"later headers delayed", &Reader{chwid: 2, smap: map[int]*meteredReader{0: nil}}, []float64{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.reader.status().Progress; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("allocated chunk progress = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReaderStatusCompletesEOFProbe(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	serve := downloadResponse([]byte("data"), fmt.Sprintf("%x", sha1.Sum([]byte("data"))))
	srv.Handle("probe", serve)
	srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
		if id := req.Query.Get("fileId"); id != "file-probe" {
			t.Errorf("download ID = %q, want file-probe", id)
		}
		return serve(req)
	})
	reader := bucket.Object("probe").NewReader(newTestCtx(t))
	reader.ChunkSize, reader.ConcurrentDownloads = 4, 1
	t.Cleanup(func() { _ = reader.Close() })
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "data" {
		t.Fatalf("download = %q, %v; want data, nil", got, err)
	}
	if got := reader.status().Progress; !reflect.DeepEqual(got, []float64{1, 1}) {
		t.Fatalf("status after EOF probe = %v, want [1 1]", got)
	}
}

func TestReaderStatusWhileResponseIsBlocked(t *testing.T) {
	for _, blocked := range []string{"bytes=0-3", "bytes=4-7"} {
		t.Run(blocked, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			ctx := newTestCtx(t)
			firstEntered, release := make(chan struct{}), make(chan struct{})
			releaseFirst := sync.OnceFunc(func() { close(release) })
			secondRead := make(chan struct{})
			reportSecond := sync.OnceFunc(func() { close(secondRead) })
			srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
				if req.Method == http.MethodHead {
					return b2wiretest.Response{Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {"sparse-id"}}}
				}
				if req.Header.Get("Range") == blocked {
					close(firstEntered)
					<-release
				}
				response := b2wiretest.RangeDownload("abcdefgh", req.Header.Get("Range"))
				response.Header.Set("X-Bz-File-Id", "sparse-id")
				return response
			})
			object := bucket.Object("sparse-status")
			object.f = bucket.b.file("sparse-id", "sparse-status")
			reader := object.NewRangeReader(ctx, 0, 8)
			reader.ChunkSize, reader.ConcurrentDownloads = 4, 2
			reader.Progress = func(n, _ int64) {
				if n >= 4 {
					reportSecond()
				}
			}
			t.Cleanup(func() { releaseFirst(); _ = reader.Close() })
			type result struct {
				body []byte
				err  error
			}
			done := make(chan result, 1)
			go func() { body, err := io.ReadAll(reader); done <- result{body, err} }()
			for _, event := range []<-chan struct{}{firstEntered, secondRead} {
				select {
				case <-event:
				case <-ctx.Done():
					t.Fatal("parallel download did not reach the blocked-response/read-other state")
				}
			}
			want := []float64{0, 1}
			if blocked == "bytes=4-7" {
				want = []float64{1, 0}
			}
			if got := reader.status().Progress; !reflect.DeepEqual(got, want) {
				t.Fatalf("out-of-order download status = %v, want %v", got, want)
			}
			releaseFirst()
			select {
			case got := <-done:
				if got.err != nil || string(got.body) != "abcdefgh" {
					t.Fatalf("download after release = %q, %v; want abcdefgh, nil", got.body, got.err)
				}
			case <-ctx.Done():
				t.Fatal("download did not finish after releasing its first response")
			}
		})
	}
}

func TestReaderPinsListedVersion(t *testing.T) {
	for _, ranged := range []bool{false, true} {
		t.Run(fmt.Sprintf("ranged progress=%t", ranged), func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("b2_list_file_versions", http.StatusOK, `{"files":[{"fileId":"old-id","fileName":"versioned","action":"upload","contentLength":8}]}`)
			serve := func(body, id string, req b2wiretest.Request) b2wiretest.Response {
				resp := downloadResponse([]byte(body), fmt.Sprintf("%x", sha1.Sum([]byte(body))))(req)
				if resp.Header == nil {
					resp.Header = make(http.Header)
				}
				resp.Header.Set("X-Bz-File-Id", id)
				return resp
			}
			srv.Handle("versioned", func(req b2wiretest.Request) b2wiretest.Response {
				return serve("newbytes", "new-id", req)
			})
			srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
				if id := req.Query.Get("fileId"); id != "old-id" {
					t.Errorf("fileId = %q, want old-id", id)
				}
				return serve("oldbytes", "old-id", req)
			})
			ctx := newTestCtx(t)
			iter := bucket.List(ctx, ListHidden())
			if !iter.Next() {
				t.Fatalf("list historical version: %v", iter.Err())
			}
			object := iter.Object()
			var reader *Reader
			want := "oldbytes"
			var samples []progressSample
			if ranged {
				reader = object.NewRangeReader(ctx, 1, 6)
				reader.Progress = func(n, total int64) { samples = append(samples, progressSample{n, total}) }
				want = "ldbyte"
			} else {
				reader = object.NewReader(ctx)
			}
			reader.ChunkSize = 3
			reader.ConcurrentDownloads = 3
			t.Cleanup(func() { _ = reader.Close() })
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != want {
				t.Fatalf("historical read = %q, %v; want %q, nil", got, err, want)
			}
			if n := len(srv.Calls("versioned")); n != 0 {
				t.Errorf("resolved version downloaded by name %d times, want 0", n)
			}
			if ranged {
				if len(samples) == 0 || samples[len(samples)-1] != (progressSample{6, 6}) {
					t.Errorf("historical range progress = %v, want final 6/6", samples)
				}
			}
		})
	}
}

func TestReaderParallelChunksPinFirstVersion(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	var mu sync.Mutex
	names := 0
	srv.Handle("parallel", func(req b2wiretest.Request) b2wiretest.Response {
		mu.Lock()
		names++
		n := names
		mu.Unlock()
		body, id := "AAAAAAAA", "idA"
		if n > 1 {
			body, id = "BBBBBBBB", "idB"
		}
		resp := b2wiretest.RangeDownload(body, req.Header.Get("Range"))
		resp.Header.Set("X-Bz-File-Id", id)
		return resp
	})
	srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
		if got := req.Query.Get("fileId"); got != "idA" {
			t.Errorf("fileId = %q, want idA", got)
		}
		resp := b2wiretest.RangeDownload("AAAAAAAA", req.Header.Get("Range"))
		resp.Header.Set("X-Bz-File-Id", "idA")
		return resp
	})
	reader := bucket.Object("parallel").NewRangeReader(newTestCtx(t), 0, 8)
	reader.ChunkSize, reader.ConcurrentDownloads = 4, 3
	t.Cleanup(func() { _ = reader.Close() })
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "AAAAAAAA" {
		t.Fatalf("parallel read = %q, %v; want AAAAAAAA, nil", got, err)
	}
	if n := len(srv.Calls("parallel")); n != 1 {
		t.Errorf("name requests = %d, want 1", n)
	}
	if n := len(srv.Calls("b2_download_file_by_id")); n != 1 {
		t.Errorf("ID requests = %d, want 1", n)
	}
}

func TestReaderRejectsMissingVersionID(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Handle("missing-id", func(req b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("data", req.Header.Get("Range"))
	})
	reader := bucket.Object("missing-id").NewRangeReader(newTestCtx(t), 0, 4)
	t.Cleanup(func() { _ = reader.Close() })
	got, err := io.ReadAll(reader)
	if len(got) != 0 || err == nil || !strings.Contains(err.Error(), "file ID") {
		t.Fatalf("read without a version ID = %q, %v; want no bytes and a file ID error", got, err)
	}
}

func TestReaderPinsBeforeParallelEOFProbes(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	serve := func(req b2wiretest.Request) b2wiretest.Response {
		resp := b2wiretest.RangeDownload("data", req.Header.Get("Range"))
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Set("X-Bz-File-Id", "tiny-id")
		return resp
	}
	srv.Handle("tiny", serve)
	srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
		if id := req.Query.Get("fileId"); id != "tiny-id" {
			t.Errorf("download ID = %q, want tiny-id", id)
		}
		return serve(req)
	})
	reader := bucket.Object("tiny").NewReader(newTestCtx(t))
	reader.ChunkSize, reader.ConcurrentDownloads = 4, 8
	t.Cleanup(func() { _ = reader.Close() })
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "data" {
		t.Fatalf("read with parallel EOF probes = %q, %v; want data, nil", got, err)
	}
	calls := srv.Calls("tiny")
	if len(calls) != 1 || calls[0].Header.Get("Range") != "bytes=0-3" {
		t.Errorf("name resolution requests = %+v, want only the first chunk bytes=0-3", calls)
	}
}

// Reader status progress is indexed by chunk id; chunk ids start
// at 0, so the first chunk must not be dropped and an active chunk must not
// read as 100%.
func TestRegression_ReaderStatusIndexesChunksFromZero(t *testing.T) {
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
func TestRegression_OverwriteDuringReadDoesNotMixVersions(t *testing.T) {
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
