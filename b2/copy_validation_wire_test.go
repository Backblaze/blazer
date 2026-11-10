package b2

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const copiedFileWireJSON = `{"accountId":"a","bucketId":"bid","fileId":"copied-id","fileName":"copied","contentLength":5,"contentSha1":"aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d","contentType":"text/plain","fileInfo":{},"action":"copy","uploadTimestamp":1}`

func TestCopyRejectsNilDestinationWire(t *testing.T) {
	key, err := NewSSECKey(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts func(*Bucket) []CopyOption
	}{
		{"nil", func(*Bucket) []CopyOption { return []CopyOption{CopyToBucket(nil)} }},
		{"nil before valid", func(b *Bucket) []CopyOption { return []CopyOption{CopyToBucket(nil), CopyToBucket(b)} }},
		{"nil after valid", func(b *Bucket) []CopyOption { return []CopyOption{CopyToBucket(b), CopyToBucket(nil)} }},
		{"before SSE-C metadata lookup", func(*Bucket) []CopyOption {
			return []CopyOption{CopyToBucket(nil), CopyDestinationSSECKey(key)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := liveOnlyBucket(t)
			// If validation regresses, these successful responses ensure an
			// unrelated service or transport error cannot satisfy the test.
			srv.Reply("b2_copy_file", http.StatusOK, copiedFileWireJSON)
			srv.Reply("b2_get_file_info", http.StatusOK, `{"fileId":"source-id","fileName":"source","contentLength":5,"contentSha1":"aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d","contentType":"text/plain","fileInfo":{},"action":"upload","uploadTimestamp":1}`)
			before := len(srv.Requests())
			object, err := bucket.Copy(context.Background(), "source-id", "copied", tc.opts(bucket)...)
			if object != nil {
				t.Errorf("Copy returned an object, want nil")
			}
			const wantError = "b2: CopyToBucket requires a non-nil destination bucket"
			if err == nil || err.Error() != wantError {
				t.Errorf("Copy error = %v, want %q", err, wantError)
			}
			if got := len(srv.Requests()) - before; got != 0 {
				t.Errorf("HTTP requests caused by invalid Copy = %d, want 0", got)
			}
		})
	}
}

// https://www.backblaze.com/apidocs/b2-copy-file documents expired_auth_token
// and reauthorization; the replay must preserve the requested copy destination.
func TestCopyReauthorizesExpiredTokenWire(t *testing.T) {
	srv := b2wiretest.New(t)
	auth := fmt.Sprintf(updateAuthJSON, srv.URL())
	srv.StrictReplySequence("b2_authorize_account",
		b2wiretest.Response{Body: auth},
		b2wiretest.Response{Body: strings.Replace(auth, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)},
	)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+updateBucketJSON+`]}`)
	srv.StrictReplySequence("b2_copy_file",
		b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`},
		b2wiretest.Response{Body: copiedFileWireJSON},
	)
	ctx := context.Background()
	client := liveOnlyClient(t, srv)
	bucket, err := client.Bucket(ctx, "n")
	if err != nil {
		t.Fatal(err)
	}
	object, err := bucket.Copy(ctx, "source-id", "copied")
	if err != nil {
		t.Fatal(err)
	}
	if object.ID() != "copied-id" || object.Name() != "copied" || object.b != bucket {
		t.Errorf("Copy returned the wrong file ID, name, or bucket")
	}
	if got := len(srv.Calls("b2_authorize_account")); got != 2 {
		t.Errorf("authorization requests = %d, want 2 including initial setup", got)
	}
	copies := srv.Calls("b2_copy_file")
	if len(copies) != 2 {
		t.Fatalf("copy requests = %d, want 2", len(copies))
	}
	for i, token := range []string{"t", "fresh"} {
		request := copies[i]
		if request.Method != http.MethodPost || request.Path != "/b2api/v4/b2_copy_file" {
			t.Errorf("copy %d = %s %s, want POST /b2api/v4/b2_copy_file", i, request.Method, request.Path)
		}
		b2wiretest.AssertHeader(t, request, "Authorization", token)
		b2wiretest.AssertJSON(t, request, "sourceFileId", "source-id")
		b2wiretest.AssertJSON(t, request, "destinationBucketId", "bid")
		b2wiretest.AssertJSON(t, request, "fileName", "copied")
	}
}
