package base

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type downloadQueryTransport struct {
	t  *testing.T
	id string
}

func (d downloadQueryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if got := req.URL.Query().Get("fileId"); got != d.id {
		d.t.Errorf("fileId changed: got %q want %q", got, d.id)
	}
	if len(req.URL.Query()) != 1 {
		d.t.Error("file ID injected extra query parameters")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"0"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func FuzzDownloadFileIDQuery(f *testing.F) {
	for _, id := range []string{"file-id", "id&other=1", "id#fragment", "id%20+space", "\r\n"} {
		f.Add(id)
	}
	f.Fuzz(func(t *testing.T, id string) {
		bucket := &Bucket{b2: &B2{downloadURI: "https://example.invalid", opts: &b2Options{transport: downloadQueryTransport{t: t, id: id}}}}
		r, err := bucket.DownloadFileByID(context.Background(), id, 0, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func FuzzFileReaderHeaders(f *testing.F) {
	for _, length := range []string{"0", "-1", "2147483648", "9223372036854775807", "bogus"} {
		f.Add(length, "Key", "value")
	}
	f.Fuzz(func(t *testing.T, length, key, value string) {
		resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
		resp.Header.Set("Content-Length", length)
		resp.Header.Set("X-Bz-Info-"+key, value)
		r, err := newFileReader(resp)
		if err != nil {
			return
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		n, parseErr := strconv.ParseInt(length, 10, 64)
		if parseErr != nil || n < 0 || int64(r.ContentLength) != n {
			t.Fatalf("invalid content length accepted: %q -> %d", length, r.ContentLength)
		}
	})
}
