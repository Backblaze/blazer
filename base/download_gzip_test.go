package base

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadGzipReturnsStoredBytes(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte("stored gzip content")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	want := compressed.Bytes()
	wantSHA1 := fmt.Sprintf("%x", sha1.Sum(want))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", fmt.Sprint(len(want)))
		w.Header().Set("X-Bz-Content-Sha1", wantSHA1)
		_, _ = w.Write(want)
	}))
	defer srv.Close()
	bucket := &Bucket{Name: "bucket", b2: &B2{downloadURI: srv.URL, authToken: "token", opts: &b2Options{}}}
	for _, tc := range []struct {
		name     string
		download func() (*FileReader, error)
	}{
		{"by ID", func() (*FileReader, error) { return bucket.DownloadFileByID(context.Background(), "id", 0, 0, false) }},
		{"by name", func() (*FileReader, error) {
			return bucket.DownloadFileByName(context.Background(), "name", 0, 0, false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fr, err := tc.download()
			if err != nil {
				t.Fatal(err)
			}
			got, readErr := io.ReadAll(fr)
			if closeErr := fr.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, want) || fr.ContentLength != len(want) || fr.SHA1 != wantSHA1 {
				t.Fatalf("download got %d bytes length=%d sha1=%q; want stored %d bytes length=%d sha1=%q", len(got), fr.ContentLength, fr.SHA1, len(want), len(want), wantSHA1)
			}
		})
	}
}
