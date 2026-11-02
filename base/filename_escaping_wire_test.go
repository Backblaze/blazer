package base

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Offline counterpart of TestUploadDownloadFilenameEscaping (which needs live
// credentials and only asserts that no error occurred): check what is actually
// put on the wire for a name containing characters that must be escaped.
const wireEscapingName = "dir/a%b+c#d?e.txt"

func TestUploadFileNameIsEscapedOnTheWire(t *testing.T) {
	var got http.Header
	transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Clone()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"fileId":"1","action":"upload"}`)), Request: req}, nil
	})
	account := &B2{opts: &b2Options{transport: transport}}
	u := &URL{uri: "https://up.invalid/upload", token: "tok", b2: account}
	if _, err := u.UploadFile(context.Background(), strings.NewReader("x"), 1, wireEscapingName, "text/plain", "sha", nil); err != nil {
		t.Fatal(err)
	}
	sent := got.Get("X-Bz-File-Name")
	for _, raw := range []string{"%b", "#", "?"} {
		if strings.Contains(sent, raw) {
			t.Errorf("X-Bz-File-Name = %q contains unescaped %q", sent, raw)
		}
	}
	for _, escaped := range []string{"%25b", "%2B", "%23", "%3F", "dir/a"} {
		if !strings.Contains(sent, escaped) {
			t.Errorf("X-Bz-File-Name = %q, want it to contain %q", sent, escaped)
		}
	}
	if back, err := unescape(sent); err != nil || back != wireEscapingName {
		t.Errorf("unescape(%q) = %q, %v, want the original name", sent, back, err)
	}
}

func TestDownloadFileNameIsEscapedInTheRequestPath(t *testing.T) {
	var got string
	transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
		got = req.URL.EscapedPath()
		if req.URL.Fragment != "" || req.URL.RawQuery != "" {
			t.Errorf("name leaked into fragment %q or query %q", req.URL.Fragment, req.URL.RawQuery)
		}
		h := make(http.Header)
		h.Set("Content-Length", "1")
		return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader("x")), Request: req}, nil
	})
	account := &B2{downloadURI: "https://dl.invalid", opts: &b2Options{transport: transport}}
	bucket := &Bucket{Name: "bkt", b2: account}
	r, err := bucket.DownloadFileByName(context.Background(), wireEscapingName, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	const want = "/file/bkt/dir/a%25b%2Bc%23d%3Fe.txt"
	if got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}
