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

//go:build knowndefect

package base

// Known-defect tests. Each test asserts the correct behaviour and currently
// fails because of an open defect, so this file is built only with the
// knowndefect tag and the default "go test" does not run it:
//
//	make known-defects
//
// When a defect is fixed, move its test into the regular suite (a file without
// the build tag, for example regression_test.go in this package) and rename
// TestKnownDefect_X to TestRegression_X.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A non-JSON error reply (for example a gateway error page) must
// still produce a non-empty error message.
func TestKnownDefect_NonJSONErrorReplyHasMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><body>Forbidden by gateway</body></html>"))
	}))
	defer srv.Close()
	_, err := AuthorizeAccount(context.Background(), "account-id", "application-key", SetAPIBase(srv.URL))
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), "b2_authorize_account: 403:"))
	if msg == "" {
		t.Errorf("error message is empty after the status code: %q", err.Error())
	}
}
