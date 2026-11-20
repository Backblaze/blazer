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
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

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
