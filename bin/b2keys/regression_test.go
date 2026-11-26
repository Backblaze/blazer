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
	"bytes"
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/subcommands"
)

func runCreate(t *testing.T, args ...string) (subcommands.ExitStatus, string) {
	t.Helper()
	c := &create{}
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	c.SetFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	status := c.Execute(context.Background(), fs)
	os.Stderr = old
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return status, buf.String()
}

// The "must be set" message must end with a newline.
func TestRegression_MissingCredentialsMessageEndsWithNewline(t *testing.T) {
	for _, name := range []string{"B2_APPLICATION_KEY_ID", "B2_APPLICATION_KEY", "B2_ACCOUNT_ID", "B2_ACCOUNT_KEY", "B2_SECRET_KEY"} {
		t.Setenv(name, "")
	}
	status, stderr := runCreate(t, "name", "listFiles")
	if status != subcommands.ExitUsageError {
		t.Fatalf("exit status = %v, want ExitUsageError", status)
	}
	if !strings.Contains(stderr, "must be set") {
		t.Fatalf("stderr = %q, want the 'must be set' message", stderr)
	}
	if !strings.HasSuffix(stderr, "\n") {
		t.Errorf("message has no line ending: %q", stderr)
	}
}

func TestLegacySecretFallbackOrder(t *testing.T) {
	for _, tc := range []struct {
		name, standard, account, secret, want string
	}{
		{"account key", "", "account-value", "", "account-value"},
		{"secret key", "", "", "secret-value", "secret-value"},
		{"secret before account", "", "account-value", "secret-value", "secret-value"},
		{"standard first", "standard-value", "account-value", "secret-value", "standard-value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("B2_APPLICATION_KEY_ID", "")
			t.Setenv("B2_ACCOUNT_ID", "legacy-id")
			t.Setenv("B2_APPLICATION_KEY", tc.standard)
			t.Setenv("B2_ACCOUNT_KEY", tc.account)
			t.Setenv("B2_SECRET_KEY", tc.secret)
			var gotID, gotKey string
			old := http.DefaultTransport
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotID, gotKey, _ = r.BasicAuth()
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":400,"code":"bad_request","message":"test"}`)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = old })
			status, _ := runCreate(t, "name", "listFiles")
			if status != subcommands.ExitFailure || gotID != "legacy-id" || gotKey != tc.want {
				t.Errorf("status=%v, id=%q, secret matched=%v; want failure after authorization with legacy id and selected secret", status, gotID, gotKey == tc.want)
			}
		})
	}
}

func TestSkillCommand(t *testing.T) {
	for _, name := range []string{apiID, apiKey, legacyAPIID, legacyAPIKey, legacyAccountKey} {
		t.Setenv(name, "")
	}
	want, err := os.ReadFile("../../.claude/skills/b2keys/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c := &skill{out: &out}
	fs := flag.NewFlagSet("skill", flag.ContinueOnError)
	if got := c.Execute(context.Background(), fs); got != subcommands.ExitSuccess {
		t.Fatalf("exit status = %v, want success", got)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatal("skill output differs from repository skill")
	}
}
