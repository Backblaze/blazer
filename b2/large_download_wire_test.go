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
	"testing"

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
				srv.StrictReplySequence("b2_get_file_info", b2wiretest.Response{Body: `{"fileId":"large-file-id","fileName":"big.bin","action":"upload","contentLength":3221225472,"contentSha1":"none","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":0}`})
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
