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
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func newSHA1WireBucket(t *testing.T) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(updateAuthJSON, srv.URL()))
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)

	ctx := context.Background()
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

// The B2 b2_upload_part docs define X-Bz-Part-Number and X-Bz-Content-Sha1:
// https://www.backblaze.com/apidocs/b2-upload-part
func TestLargeUploadSendsEachPartSHA1(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-1"}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-1","fileName":"sha1-upload","action":"upload"}`)

	payload := []byte("abcdefghijklmnopqrstuvwxy")
	writer := bucket.Object("sha1-upload").NewWriter(context.Background())
	writer.ChunkSize = 10
	writer.ConcurrentUploads = 3
	if n, err := writer.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	wantParts := map[string][]byte{
		"1": payload[0:10],
		"2": payload[10:20],
		"3": payload[20:25],
	}
	parts := srv.Calls("upload_part")
	if len(parts) != len(wantParts) {
		t.Fatalf("upload_part calls = %d, want %d", len(parts), len(wantParts))
	}
	seen := make(map[string]bool, len(wantParts))
	for _, part := range parts {
		number := part.Header.Get("X-Bz-Part-Number")
		want, ok := wantParts[number]
		if !ok {
			t.Fatalf("upload part number = %q, want 1 through 3", number)
		}
		if seen[number] {
			t.Fatalf("duplicate upload part number %q", number)
		}
		seen[number] = true
		if !bytes.Equal(part.Body, want) {
			t.Errorf("part %s body = %q, want %q", number, part.Body, want)
		}
		wantSHA1 := fmt.Sprintf("%x", sha1.Sum(want))
		if got := part.Header.Get("X-Bz-Content-Sha1"); got != wantSHA1 {
			t.Errorf("part %s X-Bz-Content-Sha1 = %q, want %q", number, got, wantSHA1)
		}
	}
}

// The b2_download_file_by_name docs define X-Bz-Content-Sha1, including the
// none value for a large file without a whole-file SHA1:
// https://www.backblaze.com/apidocs/b2-download-file-by-name
func TestReaderVerify(t *testing.T) {
	payload := []byte("fixed reader verification bytes")
	wantSHA1 := fmt.Sprintf("%x", sha1.Sum(payload))
	changed := append([]byte(nil), payload...)
	changed[0]++

	for _, tc := range []struct {
		name       string
		body       []byte
		sha1       string
		rangeStart int64
		wantErr    bool
		wantOK     bool
	}{
		{name: "verify-ok", body: payload, sha1: wantSHA1, wantOK: true},
		{name: "verify-bad", body: changed, sha1: wantSHA1, wantErr: true, wantOK: true},
		{name: "verify-range", body: payload, sha1: wantSHA1, rangeStart: 3},
		{name: "verify-none", body: payload, sha1: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Handle(tc.name, downloadResponse(tc.body, tc.sha1))

			object := bucket.Object(tc.name)
			var reader *Reader
			if tc.rangeStart > 0 {
				reader = object.NewRangeReader(context.Background(), tc.rangeStart, -1)
			} else {
				reader = object.NewReader(context.Background())
			}
			reader.ChunkSize = len(tc.body)
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			wantBody := tc.body
			if tc.rangeStart > 0 {
				wantBody = tc.body[tc.rangeStart:]
			}
			if !bytes.Equal(got, wantBody) {
				t.Fatalf("ReadAll = %q, want %q", got, wantBody)
			}
			verifyErr, ok := reader.Verify()
			if gotErr := verifyErr != nil; gotErr != tc.wantErr || ok != tc.wantOK {
				t.Errorf("Verify = (%v, %t), want error %t, ok %t", verifyErr, ok, tc.wantErr, tc.wantOK)
			}
		})
	}
}

func downloadResponse(body []byte, sha1 string) func(b2wiretest.Request) b2wiretest.Response {
	return func(req b2wiretest.Request) b2wiretest.Response {
		offset := 0
		if value := req.Header.Get("Range"); value != "" {
			start, _, ok := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
			parsed, err := strconv.Atoi(start)
			if !ok || err != nil {
				return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"invalid range"}`}
			}
			offset = parsed
		}
		if offset >= len(body) {
			return b2wiretest.Response{Status: http.StatusRequestedRangeNotSatisfiable, Body: `{"status":416,"code":"range_not_satisfiable","message":"past end"}`}
		}
		chunk := body[offset:]
		headers := make(http.Header)
		headers.Set("Content-Length", strconv.Itoa(len(chunk)))
		headers.Set("Content-Type", "application/octet-stream")
		headers.Set("X-Bz-File-Id", "file-"+req.Call)
		headers.Set("X-Bz-File-Name", req.Call)
		headers.Set("X-Bz-Content-Sha1", sha1)
		status := http.StatusOK
		if req.Header.Get("Range") != "" {
			status = http.StatusPartialContent
		}
		return b2wiretest.Response{Status: status, Header: headers, Body: string(chunk)}
	}
}
