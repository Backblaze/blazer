package b2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAPIBaseRequiresHTTPS(t *testing.T) {
	for _, test := range []struct {
		name          string
		tls, insecure bool
	}{
		{name: "http"},
		{name: "https", tls: true},
		{name: "http_opt_in", insecure: true},
		{name: "https_opt_in", tls: true, insecure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if id, key, ok := r.BasicAuth(); !ok || id != "test-id" || key != "test-key" {
					t.Error("authorization request did not contain the test credentials")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"accountId":"account","authorizationToken":"token","apiInfo":{"storageApi":{"apiUrl":"https://b2.test","downloadUrl":"https://b2.test"}}}`))
			})
			var srv *httptest.Server
			if test.tls {
				srv = httptest.NewTLSServer(handler)
			} else {
				srv = httptest.NewServer(handler)
			}
			defer srv.Close()
			endpoint := APIBase(srv.URL)
			if test.insecure {
				endpoint = InsecureAPIBase(srv.URL)
			}
			client, err := NewClient(context.Background(), "test-id", "test-key", endpoint, Transport(srv.Client().Transport))
			if test.tls || test.insecure {
				if err != nil || client == nil || calls.Load() != 1 {
					t.Fatalf("allowed endpoint: client=%v, err=%v, requests=%d; want an authorized client and one request", client != nil, err, calls.Load())
				}
			} else if err == nil || client != nil || calls.Load() != 0 {
				t.Fatalf("HTTP: client=%v, err=%v, requests=%d; want rejection before sending credentials", client != nil, err, calls.Load())
			}
		})
	}
}
