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

package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const auditResponseLimit = 64 << 20

type auditRepeatedByte struct{}

func (auditRepeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

type auditCountingBody struct {
	io.Reader
	read   int64
	closed bool
}

func (b *auditCountingBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += int64(n)
	return n, err
}
func (b *auditCountingBody) Close() error { b.closed = true; return nil }

func TestAuditC32ResponseReadsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		jsonReply bool
	}{
		{"error response", http.StatusBadRequest, false},
		{"success without decoded reply", http.StatusOK, false},
		{"success with decoded reply", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source io.Reader = io.LimitReader(auditRepeatedByte{}, auditResponseLimit+4096)
			if tc.jsonReply {
				source = io.MultiReader(strings.NewReader(`{"value":"`), source, strings.NewReader(`"}`))
			}
			body := &auditCountingBody{Reader: source}
			opts := &b2Options{transport: logRoundTrip(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Request: req, Body: body}, nil
			})}
			var reply any
			if tc.jsonReply {
				reply = &map[string]any{}
			}
			err := opts.makeRequest(context.Background(), "b2_list_buckets", http.MethodGet, "https://example.invalid", nil, reply, nil, nil)
			if body.read > auditResponseLimit+1 {
				t.Errorf("response consumed %d bytes, want at most %d", body.read, auditResponseLimit+1)
			}
			if err == nil || !strings.Contains(err.Error(), "exceeds") {
				t.Errorf("oversized response error = %v, want explicit size error", err)
			}
			if !body.closed {
				t.Error("response body was not closed")
			}
		})
	}
}

func TestAuditB26AuthDataFormattingRedactsToken(t *testing.T) {
	const token = "audit-token-sentinel-1"
	data := AuthData{AccountID: "account-visible", AuthToken: token}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		got := fmt.Sprintf(verb, data)
		if strings.Contains(got, token) || strings.Contains(strings.ToLower(got), fmt.Sprintf("%x", token)) {
			t.Errorf("%s exposed token: %s", verb, got)
		}
	}
	if data.AuthToken != token {
		t.Fatal("formatting changed the usable AuthToken")
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var restored AuthData
	if err := json.Unmarshal(encoded, &restored); err != nil || restored.AuthToken != token {
		t.Fatalf("JSON session round trip lost token: error %v", err)
	}
}

func TestAuditC45NonJSONErrorMessage(t *testing.T) {
	const message = "upstream maintenance in progress"
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", "b2_list_buckets")
	err = mkErr(&http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(message))})
	code, got := Code(err)
	if code != http.StatusBadRequest || got != message || !strings.Contains(err.Error(), message) {
		t.Fatalf("Code/error = (%d, %q), %v; want raw body %q", code, got, err, message)
	}
}

func TestAuditC32NonIdempotentOversizeIsOutcomeUnknown(t *testing.T) {
	body := &auditCountingBody{Reader: io.LimitReader(auditRepeatedByte{}, auditResponseLimit+4096)}
	opts := &b2Options{transport: logRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: body}, nil
	})}
	err := opts.makeRequest(context.Background(), "b2_create_bucket", http.MethodPost, "https://example.invalid", nil, nil, nil, nil)
	if err == nil || !errors.Is(err, ErrOutcomeUnknown) || Action(err) != OutcomeUnknown {
		t.Fatalf("oversized non-idempotent response = %v, action %v; want unknown outcome", err, Action(err))
	}
}

func TestAuditC45StructuredMessageIsPreserved(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", "b2_list_buckets")
	err = mkErr(&http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`{"status":400,"code":"bad_request","message":"structured detail"}`))})
	status, code, message := MsgCode(err)
	if status != 400 || code != "bad_request" || message != "structured detail" {
		t.Fatalf("structured error = %d, %q, %q", status, code, message)
	}
}
