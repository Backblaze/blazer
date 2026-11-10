package base

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Object Lock upload fields: https://www.backblaze.com/apidocs/b2-upload-file
// Large-file fields: https://www.backblaze.com/apidocs/b2-start-large-file
func TestFileLockArgumentCount(t *testing.T) {
	retention := &FileRetention{Mode: "governance", RetainUntilTimestamp: 1735689600000}
	for _, method := range []string{"UploadFile", "UploadFileWithSSEC", "StartLargeFile", "StartLargeFileWithSSEC"} {
		for _, tc := range []struct {
			name    string
			locks   []FileLock
			wantErr string
		}{
			{name: "zero"},
			{name: "one empty", locks: []FileLock{{}}},
			{name: "one combined", locks: []FileLock{{Retention: retention, LegalHold: "on"}}},
			{name: "two", locks: []FileLock{{Retention: retention}, {LegalHold: "on"}}, wantErr: "b2: at most one FileLock may be given, got 2"},
			{name: "three", locks: []FileLock{{Retention: retention}, {LegalHold: "on"}, {}}, wantErr: "b2: at most one FileLock may be given, got 3"},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				srv := b2wiretest.New(t)
				call := "b2_start_large_file"
				if strings.HasPrefix(method, "Upload") {
					call = "upload_file"
				}
				srv.Reply(call, http.StatusOK, `{"fileId":"file-id","fileName":"name","action":"upload"}`)
				ctx := context.Background()
				root, err := AuthorizeAccount(ctx, "account", "key", SetAPIBase(srv.URL()))
				if err != nil {
					t.Fatal(err)
				}
				upload := &URL{uri: srv.URL() + "/upload_file", token: "upload-token", b2: root}
				bucket := &Bucket{ID: "bucket-id", b2: root}
				switch method {
				case "UploadFile":
					_, err = upload.UploadFile(ctx, strings.NewReader("x"), 1, "name", "text/plain", "sha", nil, tc.locks...)
				case "UploadFileWithSSEC":
					_, err = upload.UploadFileWithSSEC(ctx, strings.NewReader("x"), 1, "name", "text/plain", "sha", nil, nil, tc.locks...)
				case "StartLargeFile":
					_, err = bucket.StartLargeFile(ctx, "name", "text/plain", nil, tc.locks...)
				case "StartLargeFileWithSSEC":
					_, err = bucket.StartLargeFileWithSSEC(ctx, "name", "text/plain", nil, nil, tc.locks...)
				}
				requests := srv.Calls(call)
				if tc.wantErr != "" {
					if err == nil || err.Error() != tc.wantErr {
						t.Errorf("error = %v, want %q", err, tc.wantErr)
					}
					if len(requests) != 0 {
						t.Errorf("%s calls = %d, want 0 for extra FileLock arguments", call, len(requests))
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(requests) != 1 {
					t.Fatalf("%s calls = %d, want 1", call, len(requests))
				}
				request := requests[0]
				if tc.name == "one combined" {
					if call == "upload_file" {
						b2wiretest.AssertHeader(t, request, "X-Bz-File-Retention-Mode", "governance")
						b2wiretest.AssertHeader(t, request, "X-Bz-File-Retention-Retain-Until-Timestamp", "1735689600000")
						b2wiretest.AssertHeader(t, request, "X-Bz-File-Legal-Hold", "on")
					} else {
						b2wiretest.AssertJSON(t, request, "fileRetention.mode", "governance")
						b2wiretest.AssertJSON(t, request, "fileRetention.retainUntilTimestamp", int64(1735689600000))
						b2wiretest.AssertJSON(t, request, "legalHold", "on")
					}
				} else if call == "upload_file" {
					for _, header := range []string{"X-Bz-File-Retention-Mode", "X-Bz-File-Retention-Retain-Until-Timestamp", "X-Bz-File-Legal-Hold"} {
						if _, ok := request.Header[http.CanonicalHeaderKey(header)]; ok {
							t.Errorf("%s present without lock settings", header)
						}
					}
				} else {
					b2wiretest.AssertNoJSON(t, request, "fileRetention")
					b2wiretest.AssertNoJSON(t, request, "legalHold")
				}
			})
		}
	}
}
