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

package main

import (
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type credentialsTransport func(*http.Request) (*http.Response, error)

func (f credentialsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLegacySecretFallbackOrder(t *testing.T) {
	for _, tc := range []struct{ name, standard, account, secret, want string }{
		{"account key", "", "account-value", "", "account-value"},
		{"secret key", "", "", "secret-value", "secret-value"},
		{"account before secret", "", "account-value", "secret-value", "account-value"},
		{"standard first", "standard-value", "account-value", "secret-value", "standard-value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("B2_APPLICATION_KEY_ID", "")
			t.Setenv("B2_ACCOUNT_ID", "legacy-id")
			t.Setenv("B2_APPLICATION_KEY", tc.standard)
			t.Setenv("B2_ACCOUNT_KEY", tc.account)
			t.Setenv("B2_SECRET_KEY", tc.secret)
			oldFlags, oldArgs, oldTransport := flag.CommandLine, os.Args, http.DefaultTransport
			t.Cleanup(func() { flag.CommandLine, os.Args, http.DefaultTransport = oldFlags, oldArgs, oldTransport })
			flag.CommandLine = flag.NewFlagSet("simple", flag.ContinueOnError)
			os.Args = []string{"simple", "source", "destination"}
			var id, key string
			http.DefaultTransport = credentialsTransport(func(r *http.Request) (*http.Response, error) {
				id, key, _ = r.BasicAuth()
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":400,"code":"bad_request","message":"test"}`)), Request: r}, nil
			})
			err := run()
			if err == nil || id != "legacy-id" || key != tc.want {
				t.Errorf("err=%v, id=%q, secret matched=%v; want failed authorization with selected credentials", err, id, key == tc.want)
			}
		})
	}
}
