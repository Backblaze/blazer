package base

import (
	"bytes"
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

func TestCopyPartOnAsLargeFile(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/b2api/v4/b2_copy_part":
			_, _ = fmt.Fprint(w, `{"fileId":"large","partNumber":1,"contentLength":5,"contentSha1":"sha"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	b, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	large := (&File{ID: "large", b2: b}).AsLargeFile()
	part, err := large.CopyPart(context.Background(), "source", 1, "bytes=0-4", nil)
	if err != nil {
		t.Fatal(err)
	}
	if part.SHA1 != "sha" || large.hashes[1] != "sha" || large.size != 5 {
		t.Fatalf("part=%+v hashes=%v size=%d", part, large.hashes, large.size)
	}
}

func TestCopyPartRetryAfterLostResponseUsesVerifiedOldSize(t *testing.T) {
	var listCalls, copyCalls int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/b2api/v4/b2_list_parts":
			listCalls++
			sha := "old"
			if copyCalls > 0 {
				sha = "new"
			}
			_, _ = fmt.Fprintf(w, `{"parts":[{"fileId":"large","partNumber":1,"contentSha1":%q,"contentLength":5000000}],"nextPartNumber":2}`, sha)
		case "/b2api/v4/b2_copy_part":
			copyCalls++
			if copyCalls == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprint(w, `{"status":503,"code":"service_unavailable","message":"response lost after copy"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"fileId":"large","partNumber":1,"contentLength":6000000,"contentSha1":"new"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	b, err := AuthorizeAccount(context.Background(), "account", "key", SetAPIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	large := (&File{ID: "large", b2: b}).CompileParts(5e6, map[int]string{1: "old"})
	if _, err := large.CopyPart(context.Background(), "src", 1, "bytes=0-5999999", nil); err == nil {
		t.Fatal("first copy unexpectedly succeeded")
	}
	if _, err := large.CopyPart(context.Background(), "src", 1, "bytes=0-5999999", nil); err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 || copyCalls != 2 || large.size != 6e6 || large.hashes[1] != "new" {
		t.Fatalf("list=%d copy=%d size=%d hashes=%v", listCalls, copyCalls, large.size, large.hashes)
	}
}

func TestCopyPartReplacementAdjustsCompiledSize(t *testing.T) {
	var listCalls int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/b2api/v4/b2_list_parts":
			listCalls++
			_, _ = fmt.Fprint(w, `{"parts":[{"fileId":"large","partNumber":1,"contentSha1":"old-one","contentLength":5000000}],"nextPartNumber":2}`)
		case "/b2api/v4/b2_copy_part":
			_, _ = fmt.Fprint(w, `{"fileId":"large","partNumber":1,"contentLength":6000000,"contentSha1":"new-one"}`)
		case "/b2api/v4/b2_finish_large_file":
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
	large := (&File{ID: "large", b2: b}).CompileParts(8e6, map[int]string{1: "old-one", 2: "old-two"})
	if _, err := large.CopyPart(context.Background(), "src", 1, "bytes=0-5999999", nil); err != nil {
		t.Fatal(err)
	}
	file, err := large.FinishLargeFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if listCalls != 1 || file.Size != 9e6 {
		t.Errorf("list calls = %d, finished size = %d; want 1, 9000000", listCalls, file.Size)
	}
}

func TestUploadPartReplacementAdjustsSize(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
		case "/part":
			if r.Method != http.MethodPost || r.Header.Get("X-Bz-Part-Number") != "1" {
				t.Errorf("upload method/part = %s/%q", r.Method, r.Header.Get("X-Bz-Part-Number"))
			}
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = fmt.Fprint(w, `{}`)
		case "/b2api/v4/b2_finish_large_file":
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
	large := (&File{ID: "large", b2: b}).CompileParts(0, nil)
	chunk := &FileChunk{file: large, url: srv.URL + "/part", token: "part-token"}
	for _, size := range []int{5e6, 6e6} {
		if _, err := chunk.UploadPart(context.Background(), bytes.NewReader(bytes.Repeat([]byte("x"), size)), "sha", size, 1); err != nil {
			t.Fatal(err)
		}
	}
	file, err := large.FinishLargeFile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if file.Size != 6e6 {
		t.Errorf("finished size = %d, want 6000000", file.Size)
	}
}

func TestCopyPartRejectsInconsistentResponseWithoutRecording(t *testing.T) {
	for _, tc := range []struct {
		name, response string
	}{
		{"wrong file", `{"fileId":"other","partNumber":1,"contentLength":5,"contentSha1":"sha"}`},
		{"wrong part", `{"fileId":"large","partNumber":2,"contentLength":5,"contentSha1":"sha"}`},
		{"missing hash", `{"fileId":"large","partNumber":1,"contentLength":5}`},
		{"empty part", `{"fileId":"large","partNumber":1,"contentLength":0,"contentSha1":"sha"}`},
		{"oversized part", `{"fileId":"large","partNumber":1,"contentLength":5000000001,"contentSha1":"sha"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/b2api/v4/b2_authorize_account":
					_, _ = fmt.Fprint(w, v4AuthJSON(srv.URL, nil, nil, ""))
				case "/b2api/v4/b2_copy_part":
					_, _ = fmt.Fprint(w, tc.response)
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
			large := (&File{ID: "large", b2: b}).CompileParts(0, nil)
			if _, err := large.CopyPart(context.Background(), "source", 1, "bytes=0-4", nil); err == nil {
				t.Fatal("inconsistent copy-part response accepted")
			}
			if len(large.hashes) != 0 || large.size != 0 {
				t.Fatalf("inconsistent response mutated large file: hashes=%v size=%d", large.hashes, large.size)
			}
		})
	}
}
