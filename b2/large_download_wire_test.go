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
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestDownloadFileByIDLargeContentLengthWire(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-download-file-by-id
	ctx := context.Background()
	srv := b2wiretest.New(t)
	bucket := newListingWireBucket(t, ctx, srv)
	srv.StrictReplySequence("b2_download_file_by_id", b2wiretest.Response{
		Header: http.Header{"Content-Length": {"3221225472"}, "Content-Type": {"application/octet-stream"}, "X-Bz-File-Id": {"large-file-id"}},
		Body:   "prefix",
	})
	reader, err := bucket.DownloadFileByID(ctx, "large-file-id")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	// This is an early-close streaming test, not a complete 3 GiB body.
	prefix := make([]byte, 6)
	if _, err := io.ReadFull(reader, prefix); err != nil || string(prefix) != "prefix" {
		t.Fatalf("download prefix=%q error=%v, want prefix, nil", prefix, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	call := srv.Last(t, "b2_download_file_by_id")
	if call.Method != http.MethodGet || call.Query.Get("fileId") != "large-file-id" || call.Header.Get("Range") != "" {
		t.Fatalf("download request=%+v, want full GET by large-file-id", call)
	}
}

func TestLargeFileHeadLookupWire(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-download-file-by-name
	for _, operation := range []string{"Attrs", "Delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			srv := b2wiretest.New(t)
			bucket := newListingWireBucket(t, ctx, srv)
			srv.StrictReplySequence("big.bin", b2wiretest.Response{Header: http.Header{
				"Content-Length": {"3221225472"}, "X-Bz-File-Id": {"large-file-id"},
			}})
			object := bucket.Object("big.bin")
			if operation == "Attrs" {
				srv.StrictReplySequence("b2_get_file_info", b2wiretest.Response{Body: `{"fileId":"large-file-id","fileName":"big.bin","action":"upload","contentLength":3221225472,"contentSha1":"none","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1700000000000}`})
				attrs, err := object.Attrs(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if attrs.Size != 3221225472 || object.ID() != "large-file-id" {
					t.Fatalf("Attrs size=%d ID=%q, want 3221225472, large-file-id", attrs.Size, object.ID())
				}
				b2wiretest.AssertJSON(t, srv.Last(t, "b2_get_file_info"), "fileId", "large-file-id")
			} else {
				srv.StrictReplySequence("b2_delete_file_version", b2wiretest.Response{Body: `{"fileId":"large-file-id","fileName":"big.bin"}`})
				if err := object.Delete(ctx); err != nil {
					t.Fatal(err)
				}
				call := srv.Last(t, "b2_delete_file_version")
				b2wiretest.AssertJSON(t, call, "fileId", "large-file-id")
				b2wiretest.AssertJSON(t, call, "fileName", "big.bin")
			}
			head := srv.Last(t, "big.bin")
			if head.Method != http.MethodHead || head.Header.Get("Range") != "" {
				t.Fatalf("lookup request=%+v, want HEAD without Range", head)
			}
		})
	}
}

func TestLargeFileRangeWithProgressWire(t *testing.T) {
	for _, tc := range []struct {
		name, byteRange, contentRange string
		offset                        int64
	}{
		{"above MaxInt32", "bytes=2500000000-2500000003", "bytes 2500000000-2500000003/3221225472", 2500000000},
		{"crossing MaxInt32", "bytes=2147483646-2147483649", "bytes 2147483646-2147483649/3221225472", 2147483646},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			srv := b2wiretest.New(t)
			bucket := newListingWireBucket(t, ctx, srv)
			srv.StrictReplySequence("big.bin",
				b2wiretest.Response{Header: http.Header{"Content-Length": {"3221225472"}}},
				b2wiretest.Response{Status: http.StatusPartialContent, Header: http.Header{
					"Content-Length": {"4"}, "Content-Range": {tc.contentRange},
				}, Body: "data"},
			)
			reader := bucket.Object("big.bin").NewRangeReader(ctx, tc.offset, 4)
			reader.ChunkSize = 4
			reader.ConcurrentDownloads = 1
			var samples []progressSample
			reader.Progress = func(complete, total int64) {
				samples = append(samples, progressSample{complete, total})
			}
			t.Cleanup(func() { _ = reader.Close() })
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if string(got) != "data" {
				t.Fatalf("range body=%q, want data", got)
			}
			wantTotal := int64(4)
			if strconv.IntSize == 32 {
				wantTotal = -1 // the HEAD compatibility length may be saturated
			}
			assertProgressSamples(t, samples, 4, wantTotal, false)
			calls := srv.Calls("big.bin")
			if len(calls) != 2 || calls[0].Method != http.MethodHead || calls[1].Method != http.MethodGet || calls[1].Header.Get("Range") != tc.byteRange {
				t.Fatalf("requests=%+v, want HEAD then GET with %s", calls, tc.byteRange)
			}
		})
	}
}
