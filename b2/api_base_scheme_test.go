// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAPIBaseLoopbackTransport(t *testing.T) {
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
			if err != nil || client == nil || calls.Load() != 1 {
				t.Fatalf("allowed endpoint: client=%v, err=%v, requests=%d; want an authorized client and one request", client != nil, err, calls.Load())
			}
		})
	}
}

// apiBaseSchemeTransport records whether option validation permits authorization.
type apiBaseSchemeTransport func(*http.Request) (*http.Response, error)

func (f apiBaseSchemeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAPIBaseSchemePolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		insecure bool
		wantErr  bool
	}{
		{name: "ipv4_loopback", endpoint: "http://127.0.0.1:8080"},
		{name: "localhost", endpoint: "http://localhost:8080"},
		{name: "localhost_upper", endpoint: "http://LOCALHOST:8080"},
		{name: "localhost_mixed", endpoint: "http://LocalHost:8080"},
		{name: "ipv6_loopback", endpoint: "http://[::1]:8080"},
		{name: "remote_http", endpoint: "http://example.com", wantErr: true},
		{name: "https", endpoint: "https://api.backblazeb2.com"},
		{name: "remote_http_opt_in", endpoint: "http://example.com", insecure: true},
		{name: "default"},
		{name: "ipv4_loopback_range", endpoint: "http://127.255.255.254:8080"},
		{name: "ipv4_mapped_loopback", endpoint: "http://[::ffff:127.0.0.1]:8080"},
		{name: "localhost_suffix", endpoint: "http://localhost.example.com:8080", wantErr: true},
		{name: "loopback_ip_suffix", endpoint: "http://127.0.0.1.example.com:8080", wantErr: true},
		{name: "localhost_userinfo", endpoint: "http://localhost@example.com:8080", wantErr: true},
		{name: "private_ipv4", endpoint: "http://192.168.1.1:8080", wantErr: true},
		{name: "unspecified_ipv4", endpoint: "http://0.0.0.0:8080", wantErr: true},
		{name: "non_loopback_ipv6", endpoint: "http://[::2]:8080", wantErr: true},
		{name: "unsupported_scheme", endpoint: "ftp://localhost:8080", wantErr: true},
		{name: "unsupported_scheme_opt_in", endpoint: "ftp://localhost:8080", insecure: true, wantErr: true},
		{name: "malformed", endpoint: "http://[::1", wantErr: true},
		{name: "malformed_opt_in", endpoint: "http://[::1", insecure: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			transport := apiBaseSchemeTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				endpoint := test.endpoint
				if endpoint == "" {
					endpoint = "https://api.backblazeb2.com"
				}
				if got, want := req.URL.String(), endpoint+"/b2api/v4/b2_authorize_account"; got != want {
					t.Errorf("authorization URL = %q, want %q", got, want)
				}
				if id, key, ok := req.BasicAuth(); !ok || id != "test-id" || key != "test-key" {
					t.Error("authorization request did not contain the test credentials")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"accountId":"account","authorizationToken":"token","apiInfo":{"storageApi":{"apiUrl":"https://b2.test","downloadUrl":"https://b2.test"}}}`)),
					Request:    req,
				}, nil
			})
			option := APIBase(test.endpoint)
			if test.insecure {
				option = InsecureAPIBase(test.endpoint)
			}
			client, err := NewClient(t.Context(), "test-id", "test-key", option, Transport(transport))
			if test.wantErr {
				if err == nil || client != nil || calls != 0 {
					t.Fatalf("rejected endpoint: client=%v, err=%v, requests=%d; want rejection before sending credentials", client != nil, err, calls)
				}
				if strings.HasPrefix(test.name, "unsupported_scheme") {
					if !strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "InsecureAPIBase") {
						t.Errorf("error %q should describe the unsupported scheme without suggesting InsecureAPIBase", err)
					}
				} else if strings.HasPrefix(test.name, "malformed") {
					if !strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "InsecureAPIBase") {
						t.Errorf("error %q should describe the invalid URL without suggesting InsecureAPIBase", err)
					}
				} else if !strings.Contains(err.Error(), "InsecureAPIBase") {
					t.Errorf("error %q does not explain the HTTP opt-in", err)
				}
				return
			}
			if err != nil || client == nil || calls != 1 {
				t.Fatalf("allowed endpoint: client=%v, err=%v, requests=%d; want an authorized client and one request", client != nil, err, calls)
			}
		})
	}
}

func TestRestoredSessionSchemePolicy(t *testing.T) {
	const (
		authorizeURL = "https://authorize.example"
		badURL       = "http://attacker.example"
		cachedToken  = "session-token"
		freshToken   = "fresh-token"
	)
	tests := []struct {
		name     string
		change   func(*AuthData)
		insecure bool
		reuse    bool
	}{
		{name: "HTTP API address", change: func(data *AuthData) { data.APIURL = badURL }},
		{name: "HTTP download address", change: func(data *AuthData) { data.DownloadURL = badURL }},
		{name: "HTTP S3 address", change: func(data *AuthData) { data.S3URL = badURL }},
		{name: "HTTP API address with opt-in", change: func(data *AuthData) { data.APIURL = badURL }, insecure: true, reuse: true},
		{name: "HTTP download address with opt-in", change: func(data *AuthData) { data.DownloadURL = badURL }, insecure: true, reuse: true},
		{name: "HTTP S3 address with opt-in", change: func(data *AuthData) { data.S3URL = badURL }, insecure: true, reuse: true},
		{name: "HTTPS addresses", change: func(*AuthData) {}, reuse: true},
		{name: "loopback HTTP addresses", change: func(data *AuthData) {
			data.APIURL = "http://127.0.0.1:8080"
			data.DownloadURL = "http://localhost:8080"
			data.S3URL = "http://[::1]:8080"
		}, reuse: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &InMemoryAccountInfo{}
			data := sampleAuthData()
			test.change(&data)
			store.Set(data)

			authorizeCalls := 0
			var requests []string
			transport := apiBaseSchemeTransport(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				if req.URL.Path != "/b2api/v4/b2_authorize_account" {
					t.Fatalf("unexpected request to %s", req.URL)
				}
				authorizeCalls++
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"accountId":"account","authorizationToken":"` + freshToken +
						`","apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"recommendedPartSize":100000000,` +
						`"apiUrl":"https://api.example","downloadUrl":"https://download.example","s3ApiUrl":"https://s3.example"}}}`)),
					Request: req,
				}, nil
			})
			endpoint := APIBase(authorizeURL)
			if test.insecure {
				endpoint = InsecureAPIBase(authorizeURL)
			}
			client, err := NewClient(t.Context(), "key-id", "application-key-secret", endpoint, Transport(transport), WithAccountInfo(store))
			if err != nil {
				t.Fatal(err)
			}
			wantAuthorizeCalls, wantToken := 1, freshToken
			if test.reuse {
				wantAuthorizeCalls, wantToken = 0, cachedToken
			}
			if authorizeCalls != wantAuthorizeCalls {
				t.Errorf("authorization calls = %d, want %d", authorizeCalls, wantAuthorizeCalls)
			}
			got := client.backend.(*beRoot).b2i.(*b2Root).authData()
			if got.AuthToken != wantToken {
				t.Errorf("restored token = %q, want %q", got.AuthToken, wantToken)
			}
			if !test.reuse {
				if len(requests) == 0 || requests[0] != authorizeURL+"/b2api/v4/b2_authorize_account" {
					t.Errorf("first request = %v, want authorization at %s", requests, authorizeURL)
				}
				for _, request := range requests {
					if strings.HasPrefix(request, badURL) {
						t.Errorf("request sent to rejected address: %s", request)
					}
				}
			}
		})
	}
}
