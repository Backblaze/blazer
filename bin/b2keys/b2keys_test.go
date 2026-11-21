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
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/subcommands"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCreateRejectsNegativeDurationBeforeRequest(t *testing.T) {
	t.Setenv(apiID, "test-id")
	t.Setenv(apiKey, "test-key")
	requests := 0
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{Request: r, StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":400,"code":"bad_request","message":"unexpected request"}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })

	c := &create{}
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	c.SetFlags(fs)
	if err := fs.Parse([]string{"-duration=-1s", "name", "listFiles"}); err != nil {
		t.Fatal(err)
	}
	if got := c.Execute(context.Background(), fs); got != subcommands.ExitUsageError {
		t.Errorf("exit status = %v, want %v (usage error)", got, subcommands.ExitUsageError)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0", requests)
	}
}

func TestCreateDurationOnWire(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		duration string
	}{
		{name: "default"},
		{name: "one_hour", args: []string{"-duration=1h"}, duration: "3600"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(apiID, "test-id")
			t.Setenv(apiKey, "test-key")
			var calls []string
			previous := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Path)
				var body string
				switch r.URL.Path {
				case "/b2api/v4/b2_authorize_account":
					body = `{"accountId":"account-id","authorizationToken":"auth-token","apiInfo":{"storageApi":{"apiUrl":"https://b2.invalid","downloadUrl":"https://b2.invalid"}}}`
				case "/b2api/v3/b2_create_key":
					var request map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					if got := request["validDurationInSeconds"]; string(got) != test.duration {
						t.Errorf("validDurationInSeconds = %s, want %s (empty means omitted)", got, test.duration)
					}
					body = `{"accountId":"account-id","applicationKeyId":"created-id","applicationKey":"test-secret","keyName":"name","capabilities":["listFiles"]}`
				default:
					t.Fatalf("unexpected request: %s", r.URL.Path)
				}
				return &http.Response{Request: r, StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })

			c := &create{}
			fs := flag.NewFlagSet("create", flag.ContinueOnError)
			c.SetFlags(fs)
			if err := fs.Parse(append(test.args, "name", "listFiles")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if got := c.Execute(ctx, fs); got != subcommands.ExitSuccess {
				t.Errorf("exit status = %v, want %v (success)", got, subcommands.ExitSuccess)
			}
			if got := strings.Join(calls, ","); got != "/b2api/v4/b2_authorize_account,/b2api/v3/b2_create_key" {
				t.Errorf("calls = %q, want authorization then key creation", got)
			}
		})
	}
}
