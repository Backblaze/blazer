package base

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialContainersFormatDuringPartUploads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	account := NewAuthorizedAccount(AuthData{AuthToken: "b2sim-token"})
	large := (&File{ID: "large-287", b2: account}).AsLargeFile()
	chunk := &FileChunk{url: server.URL, token: "part-token", file: large}
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 64; i++ {
			n, err := chunk.UploadPart(t.Context(), strings.NewReader("x"), "sha1", 1, i)
			if err != nil {
				done <- err
				return
			}
			if n != 1 {
				done <- fmt.Errorf("uploaded %d bytes, want 1", n)
				return
			}
		}
		done <- nil
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
				got := fmt.Sprintf(verb, large)
				if !strings.Contains(got, "ID:large-287") || strings.Contains(got, "b2sim-token") {
					t.Errorf("unsafe or missing upload identity for %s: %s", verb, got)
				}
			}
		}
	}
}
