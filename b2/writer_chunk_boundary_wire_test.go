package b2

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestWriterChunkBoundaryWire(t *testing.T) {
	// The recording server does not enforce B2's 5 MB minimum part size;
	// use that real minimum here so these boundaries also describe valid uploads.
	const chunkSize = 5_000_000
	for _, path := range []struct {
		name string
		copy func(*Writer, []byte) (int64, error)
	}{
		{"Write", func(w *Writer, payload []byte) (int64, error) {
			// Hide ReaderFrom and Seek/WriteTo so io.Copy exercises Write.
			// Split at the first boundary to exercise the next Write with a full buffer.
			source := bytes.NewReader(payload)
			reader := io.MultiReader(io.LimitReader(source, chunkSize), source)
			return io.Copy(struct{ io.Writer }{w}, struct{ io.Reader }{reader})
		}},
		{"ReadFrom", func(w *Writer, payload []byte) (int64, error) {
			return w.ReadFrom(bytes.NewReader(payload))
		}},
	} {
		t.Run(path.name, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				size      int
				partSizes []int
			}{
				{"ChunkSize-1", chunkSize - 1, []int{chunkSize - 1}},
				{"ChunkSize", chunkSize, []int{chunkSize}},
				{"ChunkSize+1", chunkSize + 1, []int{chunkSize, 1}},
				{"2*ChunkSize", 2 * chunkSize, []int{chunkSize, chunkSize}},
				{"2*ChunkSize+1", 2*chunkSize + 1, []int{chunkSize, chunkSize, 1}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv := b2wiretest.New(t)
					srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
					srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
					srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
					srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"object","action":"upload"}`)

					ctx := context.Background()
					client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
					if err != nil {
						t.Fatal(err)
					}
					bucket, err := client.NewBucket(ctx, "bucket", &BucketAttrs{Type: Private})
					if err != nil {
						t.Fatal(err)
					}
					before := len(srv.Requests())
					writer := bucket.Object("object").NewWriter(ctx)
					writer.ChunkSize = chunkSize
					writer.ConcurrentUploads = 1
					payload := make([]byte, tc.size)
					for i := range payload {
						payload[i] = byte(i % 251)
					}
					if n, err := path.copy(writer, payload); err != nil || n != int64(tc.size) {
						t.Fatalf("%s = %d, %v; want %d, nil", path.name, n, err, tc.size)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}

					wantCalls := []string{"b2_get_upload_url", "upload_file"}
					if len(tc.partSizes) > 1 {
						wantCalls = []string{"b2_start_large_file", "b2_get_upload_part_url"}
						for range tc.partSizes {
							wantCalls = append(wantCalls, "upload_part")
						}
						wantCalls = append(wantCalls, "b2_finish_large_file")
					}
					var calls []string
					var partSizes []int
					var uploaded []byte
					for _, request := range srv.Requests()[before:] {
						calls = append(calls, request.Call)
						if request.Call != "upload_file" && request.Call != "upload_part" {
							continue
						}
						body := request.Body
						if request.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" {
							if len(body) < 40 {
								t.Fatalf("%s body has %d bytes, want a 40-byte SHA1 trailer", request.Call, len(body))
							}
							body = body[:len(body)-40]
						}
						partSizes = append(partSizes, len(body))
						uploaded = append(uploaded, body...)
					}
					if !reflect.DeepEqual(calls, wantCalls) {
						t.Errorf("upload calls = %v, want %v", calls, wantCalls)
					}
					if !reflect.DeepEqual(partSizes, tc.partSizes) {
						t.Errorf("upload sizes = %v, want %v", partSizes, tc.partSizes)
					}
					if !bytes.Equal(uploaded, payload) {
						t.Errorf("uploaded bytes differ from input: got %d bytes, want %d", len(uploaded), len(payload))
					}
				})
			}
		})
	}
}
