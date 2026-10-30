package b2

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Empty uploads must stay empty in the request, metadata and downloaded bytes.
func TestZeroByteObjectWire(t *testing.T) {
	for _, chunkSize := range []int{0, 100000000} {
		name := "default-chunk-size"
		if chunkSize != 0 {
			name = "explicit-chunk-size"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv, bucket := newSHA1WireBucket(t)
			const file = `{"fileId":"empty-id","fileName":"empty","action":"upload","contentLength":0,"contentSha1":"da39a3ee5e6b4b0d3255bfef95601890afd80709","contentType":"application/octet-stream","fileInfo":{},"uploadTimestamp":1}`
			srv.Reply("upload_file", http.StatusOK, file)
			srv.Reply("b2_get_file_info", http.StatusOK, file)
			srv.Handle("empty", func(r b2wiretest.Request) b2wiretest.Response {
				if r.Method != http.MethodHead {
					return downloadResponse(nil, "da39a3ee5e6b4b0d3255bfef95601890afd80709")(r)
				}
				return b2wiretest.Response{Header: http.Header{
					"Content-Length":    {"0"},
					"X-Bz-File-Id":      {"empty-id"},
					"X-Bz-Content-Sha1": {"da39a3ee5e6b4b0d3255bfef95601890afd80709"},
				}}
			})
			writer := bucket.Object("empty").NewWriter(ctx)
			writer.ChunkSize = chunkSize
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			uploads := srv.Calls("upload_file")
			if len(uploads) != 1 {
				t.Fatalf("upload calls = %d, want 1", len(uploads))
			}
			if got := uploads[0]; len(got.Body) != 0 || got.Header.Get("Content-Length") != "0" || got.Header.Get("X-Bz-File-Name") != "empty" {
				t.Fatalf("empty upload: body=%q headers=%v", got.Body, got.Header)
			}
			attrs, err := bucket.Object("empty").Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if attrs.Size != 0 {
				t.Fatalf("empty object size = %d, want 0", attrs.Size)
			}
			b2wiretest.AssertJSON(t, srv.Last(t, "b2_get_file_info"), "fileId", "empty-id")
			reader := bucket.Object("empty").NewReader(ctx)
			defer reader.Close()
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if len(data) != 0 {
				t.Fatalf("empty object content = %q, want empty", data)
			}
		})
	}
}

// Delete resolves a version by HEAD, sends its ID and name, then a
// fresh handle must observe that the file is gone.
func TestDeleteWithoutNameWire(t *testing.T) {
	ctx := context.Background()
	srv := b2wiretest.New(t)
	bucket := newListingWireBucket(t, ctx, srv)
	deleted := make(chan struct{})
	srv.Handle("victim", func(r b2wiretest.Request) b2wiretest.Response {
		select {
		case <-deleted:
			return b2wiretest.Response{Status: http.StatusNotFound,
				Body: `{"status":404,"code":"not_found","message":"file not present"}`}
		default:
			return b2wiretest.Response{Header: http.Header{
				"Content-Length": {"7"}, "X-Bz-File-Id": {"victim-id"},
			}, Body: "payload"}
		}
	})
	srv.Reply("b2_get_file_info", http.StatusOK, `{"fileId":"victim-id","fileName":"victim","action":"upload","contentLength":7,"contentSha1":"none","contentType":"text/plain","fileInfo":{},"uploadTimestamp":1}`)
	srv.Handle("b2_delete_file_version", func(r b2wiretest.Request) b2wiretest.Response {
		if r.JSON["fileId"] != "victim-id" || r.JSON["fileName"] != "victim" {
			return b2wiretest.Response{Status: http.StatusBadRequest,
				Body: `{"status":400,"code":"bad_request","message":"wrong version"}`}
		}
		select {
		case <-deleted:
			t.Error("duplicate delete")
		default:
			close(deleted)
		}
		return b2wiretest.Response{Body: `{"fileId":"victim-id","fileName":"victim"}`}
	})
	if _, err := bucket.Object("victim").Attrs(ctx); err != nil {
		t.Fatalf("Attrs before Delete: %v", err)
	}
	if err := bucket.Object("victim").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Object("victim").Attrs(ctx); !IsNotExist(err) {
		t.Errorf("Attrs after Delete = %v, want not-exist", err)
	}
	deletes := srv.Calls("b2_delete_file_version")
	if len(deletes) != 1 {
		t.Fatalf("delete calls = %d, want 1", len(deletes))
	}
	b2wiretest.AssertJSON(t, deletes[0], "fileId", "victim-id")
	b2wiretest.AssertJSON(t, deletes[0], "fileName", "victim")
	lookups := srv.Calls("victim")
	if len(lookups) != 3 {
		t.Fatalf("HEAD calls = %d, want 3 (before, resolve for delete, after)", len(lookups))
	}
	for _, request := range lookups {
		if request.Method != http.MethodHead {
			t.Errorf("lookup method = %s, want HEAD", request.Method)
		}
	}
}
