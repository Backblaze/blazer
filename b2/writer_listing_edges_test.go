package b2

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestMappingSSECKeyIDCaseConflict(t *testing.T) {
	// https://www.backblaze.com/docs/cloud-storage-server-side-encryption
	wire := newSSECPublicWire(t)
	bucket := wire.newBucket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name, second string
		valid        bool
	}{{"conflicting", "b", false}, {"equal", "a", true}} {
		t.Run(tc.name, func(t *testing.T) {
			// No ID makes a case conflict distinguishable from the later missing-ID error.
			key := wire.key
			if tc.valid {
				key = wire.keyWithID(t, "a")
			}
			writer := bucket.Object(tc.name).NewWriter(ctx,
				WithSSECKey(key),
				WithAttrsOption(&Attrs{Info: map[string]string{"sse_c_key_id": "a", "SSE_C_KEY_ID": tc.second}}))
			before := len(wire.requestsTo("/b2api/v4/b2_start_large_file"))
			_, err := writer.getLargeFile()
			if tc.valid {
				if err != nil {
					t.Fatalf("getLargeFile() = %v, want nil", err)
				}
				if got := len(wire.requestsTo("/b2api/v4/b2_start_large_file")); got != before+1 {
					t.Fatalf("start requests = %d, want %d", got, before+1)
				}
				requests := wire.requestsTo("/b2api/v4/b2_start_large_file")
				if got := requests[len(requests)-1].body["fileInfo"]; !reflect.DeepEqual(got, map[string]any{"sse_c_key_id": "a"}) {
					t.Fatalf("fileInfo = %v, want map[sse_c_key_id:a]", got)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "sse_c_key_id") || !strings.Contains(err.Error(), "conflicts") {
				t.Fatalf("getLargeFile() = %v, want SSE-C key ID conflict", err)
			}
			if got := len(wire.requestsTo("/b2api/v4/b2_start_large_file")); got != before {
				t.Fatalf("start requests = %d, want %d", got, before)
			}
			if got := len(wire.requestsTo("/upload")) + len(wire.requestsTo("/upload-part")); got != 0 {
				t.Fatalf("upload requests = %d, want 0", got)
			}
		})
	}
}

func TestMappingListPageSizeClamps(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-file-names
	// https://www.backblaze.com/apidocs/b2-list-unfinished-large-files
	for _, tc := range []struct {
		name        string
		given, want int
		unfinished  bool
	}{{"large", 5000, 1000, false}, {"negative", -1, 1000, false}, {"within range", 500, 500, false}, {"unfinished", 500, 100, true}} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_file_names", http.StatusOK, `{"files":[],"nextFileName":null}`)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[],"nextFileId":null}`)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			opts := []ListOption{ListPageSize(tc.given)}
			call := "b2_list_file_names"
			if tc.unfinished {
				opts = append(opts, ListUnfinished())
				call = "b2_list_unfinished_large_files"
			}
			iter := bucket.List(ctx, opts...)
			for iter.Next() {
			}
			if err := iter.Err(); err != nil {
				t.Fatal(err)
			}
			calls := srv.Calls(call)
			if len(calls) != 1 {
				t.Fatalf("%s requests = %d, want 1", call, len(calls))
			}
			b2wiretest.AssertJSON(t, calls[0], "maxFileCount", tc.want)
		})
	}
}

func TestMappingStatusCleanup(t *testing.T) {
	// Client.Status documents active readers and writers only.
	_, bucket := newProgressWire(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := bucket.Object("status").NewWriter(ctx)
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if got := len(bucket.c.Status().Writers); got != 1 {
		t.Fatalf("active writers = %d, want 1", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(bucket.c.Status().Writers); got != 0 {
		t.Fatalf("closed writers = %d, want 0", got)
	}
	r := bucket.Object("status").NewReader(ctx)
	buf := make([]byte, 1)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	if got := len(bucket.c.Status().Readers); got != 1 {
		t.Fatalf("active readers = %d, want 1", got)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(bucket.c.Status().Readers); got != 0 {
		t.Fatalf("closed readers = %d, want 0", got)
	}
}

func TestMappingFileInfoEntryLimit(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-upload-file
	for _, count := range []int{10, 9} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			user := make(map[string]string)
			for i := 0; i < count; i++ {
				user[fmt.Sprintf("user_%d", i)] = "value"
			}
			attrs := &Attrs{Info: user, SHA1: strings.Repeat("a", 40), LastModified: time.Unix(123, 0)}
			got := fileInfoFromAttrs(attrs, 10)
			if len(got) != 10 {
				t.Fatalf("file info entries = %d, want 10", len(got))
			}
			for k, v := range user {
				if got[k] != v {
					t.Fatalf("file info %s = %q, want %q", k, got[k], v)
				}
			}
			_, hasSHA := got["large_file_sha1"]
			if hasSHA != (count == 9) {
				t.Fatalf("large_file_sha1 present = %v, want %v", hasSHA, count == 9)
			}
			if _, hasModified := got["src_last_modified_millis"]; hasModified {
				t.Fatal("src_last_modified_millis present, want absent")
			}
		})
	}
}

func TestMappingReadFromChunkBoundary(t *testing.T) {
	// Writer.ChunkSize documents the simple/large-file boundary; B2 requires at least two parts.
	// https://www.backblaze.com/docs/cloud-storage-create-large-files-with-the-native-api
	// The exact ChunkSize boundary is tracked separately in #221.
	for _, tc := range []struct {
		name  string
		size  int
		large bool
	}{{"below", 4_999_999, false}, {"above", 5_000_001, true}} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"boundary","action":"upload","uploadTimestamp":1}`)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			payload := bytes.Repeat([]byte("x"), tc.size)
			w := bucket.Object("boundary").NewWriter(ctx)
			w.ChunkSize = 5_000_000
			if n, err := w.ReadFrom(bytes.NewReader(payload)); err != nil || n != int64(tc.size) {
				t.Fatalf("ReadFrom = %d, %v; want %d, nil", n, err, tc.size)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			var endpoints []string
			var uploaded []byte
			for _, req := range srv.Requests() {
				switch req.Call {
				case "upload_file", "upload_part":
					endpoints = append(endpoints, req.Call)
					data := req.Body
					if req.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" {
						data = data[:len(data)-40]
					}
					uploaded = append(uploaded, data...)
				case "b2_start_large_file", "b2_finish_large_file", "b2_get_upload_url", "b2_get_upload_part_url":
					endpoints = append(endpoints, req.Call)
				}
			}
			wantEndpoints := []string{"b2_get_upload_url", "upload_file"}
			if tc.large {
				wantEndpoints = []string{"b2_start_large_file", "b2_get_upload_part_url", "upload_part", "upload_part", "b2_finish_large_file"}
			}
			if !reflect.DeepEqual(endpoints, wantEndpoints) {
				t.Fatalf("endpoints = %v, want %v", endpoints, wantEndpoints)
			}
			if !bytes.Equal(uploaded, payload) {
				t.Fatalf("uploaded bytes = %d/match=%v, want %d/match=true", len(uploaded), bytes.Equal(uploaded, payload), tc.size)
			}
		})
	}
}

func TestMappingResumeListsEveryPartPage(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-parts: nextPartNumber starts the next page.
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"unfinished-id","fileName":"resume","action":"start","fileInfo":{}}],"nextFileId":null}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"unfinished-id","fileName":"resume","action":"upload","uploadTimestamp":1}`)
	part := bytes.Repeat([]byte("r"), 5_000_000)
	digest := fmt.Sprintf("%x", sha1.Sum(part))
	page := func(first, last int, next any) string {
		var parts []map[string]any
		for number := first; number <= last; number++ {
			parts = append(parts, map[string]any{"partNumber": number, "contentLength": 5_000_000, "contentSha1": digest})
		}
		body, err := json.Marshal(map[string]any{"parts": parts, "nextPartNumber": next})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	first, second := page(1, 100, 101), page(101, 103, nil)
	srv.Handle("b2_list_parts", func(r b2wiretest.Request) b2wiretest.Response {
		switch r.JSON["startPartNumber"] {
		case float64(1):
			return b2wiretest.Response{Status: http.StatusOK, Body: first}
		case float64(101):
			return b2wiretest.Response{Status: http.StatusOK, Body: second}
		default:
			return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"wrong part cursor"}`}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w := bucket.Object("resume").NewWriter(ctx)
	w.Resume = true
	w.ChunkSize = 5_000_000
	var readers []io.Reader
	for i := 0; i < 103; i++ {
		readers = append(readers, bytes.NewReader(part))
	}
	if n, err := w.ReadFrom(io.MultiReader(readers...)); err != nil || n != 515_000_000 {
		t.Fatalf("ReadFrom = %d, %v; want 515000000, nil", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	calls := srv.Calls("b2_list_parts")
	if len(calls) != 2 {
		t.Fatalf("list-parts requests = %d, want 2", len(calls))
	}
	for i, start := range []int{1, 101} {
		b2wiretest.AssertJSON(t, calls[i], "startPartNumber", start)
		b2wiretest.AssertJSON(t, calls[i], "maxPartCount", 100)
		b2wiretest.AssertJSON(t, calls[i], "fileId", "unfinished-id")
	}
	if got := len(srv.Calls("upload_part")); got != 0 {
		t.Fatalf("upload-part requests = %d, want 0 for existing parts 1 through 103", got)
	}
	wantHashes := make([]string, 103)
	for i := range wantHashes {
		wantHashes[i] = digest
	}
	b2wiretest.AssertJSON(t, srv.Last(t, "b2_finish_large_file"), "partSha1Array", wantHashes)
}
