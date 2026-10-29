package b2

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const (
	wireContentType    = "text/html; charset=utf-8"
	defaultContentType = "application/octet-stream"
)

type uploadContentTypeWire struct {
	bucket *Bucket
	server *b2wiretest.Server
}

func newUploadContentTypeWire(t *testing.T) uploadContentTypeWire {
	t.Helper()
	server := b2wiretest.New(t)
	server.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	server.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
	server.Reply("upload_file", http.StatusOK, `{"fileId":"small-1","fileName":"small","action":"upload","uploadTimestamp":1}`)
	server.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-1"}`)
	server.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-1","fileName":"large","action":"upload","uploadTimestamp":1}`)

	ctx := context.Background()
	client, err := NewClient(ctx, "account", "key", APIBase(server.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "bucket", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return uploadContentTypeWire{bucket: bucket, server: server}
}

func TestWriterSendsContentType(t *testing.T) {
	// b2_upload_file requires the Content-Type header and b2_start_large_file
	// requires the contentType body field. See:
	// https://www.backblaze.com/apidocs/b2-upload-file
	// https://www.backblaze.com/apidocs/b2-start-large-file
	for _, tc := range []struct {
		name  string
		large bool
		opts  []WriterOption
		want  string
	}{
		{name: "small explicit", opts: []WriterOption{WithAttrsOption(&Attrs{ContentType: wireContentType})}, want: wireContentType},
		{name: "small default", want: defaultContentType},
		{name: "large explicit", large: true, opts: []WriterOption{WithAttrsOption(&Attrs{ContentType: wireContentType})}, want: wireContentType},
		{name: "large default", large: true, want: defaultContentType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := newUploadContentTypeWire(t)
			writer := wire.bucket.Object("object").NewWriter(context.Background(), tc.opts...)
			payload := []byte("0123456789")
			if tc.large {
				// 10 is intentionally far below Writer's documented 5 MB minimum part size, used only
				// to force the multi-part (b2_start_large_file) path in this wire test. The b2wiretest
				// recording server does not enforce B2's minimum, so this is valid here but not as a
				// real-world ChunkSize.
				writer.ChunkSize = 10
				payload = append(bytes.Repeat(payload, 2), []byte("01234")...)
			}
			if _, err := writer.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			if tc.large {
				starts := wire.server.Calls("b2_start_large_file")
				if len(starts) != 1 {
					t.Fatalf("b2_start_large_file calls = %d, want 1", len(starts))
				}
				b2wiretest.AssertJSON(t, starts[0], "contentType", tc.want)
				return
			}
			uploads := wire.server.Calls("upload_file")
			if len(uploads) != 1 {
				t.Fatalf("upload_file calls = %d, want 1", len(uploads))
			}
			b2wiretest.AssertHeader(t, uploads[0], "Content-Type", tc.want)
		})
	}
}
