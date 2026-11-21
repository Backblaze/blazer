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
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"

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
