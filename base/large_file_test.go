package base

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestStartLargeFileReturnsRequestResult(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
	}{
		{"success", `{"fileId":"large-id"}`, http.StatusOK},
		{"failure", `{"status":400,"code":"bad_request","message":"start rejected"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Reply("b2_start_large_file", tc.status, tc.response)
			root, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			large, err := (&Bucket{ID: "bid", b2: root}).StartLargeFile(ctx, "large", "text/plain", nil)
			if tc.status == http.StatusBadRequest {
				code, _ := Code(err)
				if large != nil || code != 400 || err == nil || !strings.Contains(err.Error(), "start rejected") {
					t.Fatalf("StartLargeFile = (%v, %v), status %d; want nil and start rejected (400)", large, err, code)
				}
			} else if err != nil || large == nil || large.ID != "large-id" {
				t.Fatalf("StartLargeFile = (%v, %v), want large-id and nil", large, err)
			}
			b2wiretest.AssertJSON(t, srv.Last(t, "b2_start_large_file"), "bucketId", "bid")
		})
	}
}

func TestUploadPartPreservesExplicitAndTrailingSHA1(t *testing.T) {
	for _, tc := range []struct {
		name, body, checksum string
		size                 int
	}{
		{"explicit", "abc", "a9993e364706816aba3e25717850c26c9cd0d89d", 3},
		{"trailing", "abca9993e364706816aba3e25717850c26c9cd0d89d", "hex_digits_at_end", 43},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"large-id","fileName":"large","action":"upload"}`)
			root, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			large := (&File{ID: "large-id", b2: root}).AsLargeFile()
			chunk, err := large.GetUploadPartURL(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := chunk.UploadPart(ctx, strings.NewReader(tc.body), tc.checksum, tc.size, 1); err != nil || n != tc.size {
				t.Fatalf("UploadPart = (%d, %v), want %d and nil", n, err, tc.size)
			}
			request := srv.Last(t, "upload_part")
			if string(request.Body) != tc.body || request.Header.Get("X-Bz-Content-Sha1") != tc.checksum {
				t.Errorf("upload body=%q checksum=%q, want %q and %q", request.Body, request.Header.Get("X-Bz-Content-Sha1"), tc.body, tc.checksum)
			}
			if _, err := large.FinishLargeFile(ctx); err != nil {
				t.Fatal(err)
			}
			if got := srv.Last(t, "b2_finish_large_file").JSON["partSha1Array"]; !reflect.DeepEqual(got, []any{"a9993e364706816aba3e25717850c26c9cd0d89d"}) {
				t.Errorf("finish partSha1Array = %#v, want [abc SHA1]", got)
			}
		})
	}
}
