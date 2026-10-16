package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCopyPartRecordsHashAndSizeForFinish(t *testing.T) {
	var copied map[string]any
	var finished map[string]any
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/b2api/v4/b2_copy_part":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "auth-token" {
				t.Errorf("copy method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &copied); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprint(w, `{"fileId":"large","partNumber":2,"contentLength":10,"contentSha1":"copy-sha"}`)
		case "/b2api/v4/b2_finish_large_file":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &finished); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprint(w, `{"fileId":"large","fileName":"dst","action":"upload"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	large := (&File{ID: "large", b2: b}).CompileParts(5e6, map[int]string{1: "upload-sha"})
	part, err := large.CopyPart(context.Background(), "src", 2, "bytes=0-9", nil)
	if err != nil {
		t.Fatal(err)
	}
	if part.Number != 2 || part.SHA1 != "copy-sha" || part.Size != 10 {
		t.Errorf("part = %+v", part)
	}
	if want := map[string]any{"sourceFileId": "src", "largeFileId": "large", "partNumber": float64(2), "range": "bytes=0-9"}; !reflect.DeepEqual(copied, want) {
		t.Errorf("copy request = %#v, want %#v", copied, want)
	}
	file, err := large.FinishLargeFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if file.Size != 5e6+10 || !reflect.DeepEqual(finished["partSha1Array"], []any{"upload-sha", "copy-sha"}) {
		t.Errorf("finish size=%d body=%v", file.Size, finished)
	}
}
