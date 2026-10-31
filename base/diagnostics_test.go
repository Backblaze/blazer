package base

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type completeErrorThenEOFBody struct{}

func (completeErrorThenEOFBody) Read(p []byte) (int, error) {
	n := copy(p, `{"status":400,"code":"bad_request","message":"literal response"}`)
	return n, io.ErrUnexpectedEOF
}

func (completeErrorThenEOFBody) Close() error { return nil }

func TestMkErrRetainsResponseReadError(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/b2api/v4/b2_delete_file_version", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", "b2_delete_file_version")
	err = mkErr(&http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       completeErrorThenEOFBody{},
		Request:    req,
	})
	if err == nil || !strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
		t.Fatalf("mkErr error = %v, want %q", err, io.ErrUnexpectedEOF)
	}
}

func TestMkErrRetryAfterWarning(t *testing.T) {
	if os.Getenv("BLAZER_RETRY_AFTER_LOG_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestMkErrRetryAfterWarning$")
		command.Env = append(os.Environ(), "B2_LOG_LEVEL=1", "BLAZER_RETRY_AFTER_LOG_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("retry-after log test failed: %v\n%s", err, output)
		}
		return
	}
	var output bytes.Buffer
	log.SetOutput(&output)
	newResponse := func(retryAfter string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, "https://example.invalid/b2api/v4/b2_delete_file_version", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Blazer-Method", "b2_delete_file_version")
		req.Header.Set("Authorization", "request-key-secret")
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Retry-After":   {retryAfter},
				"Authorization": {"response-token-secret"},
			},
			Body:    io.NopCloser(strings.NewReader(`{"status":429,"code":"too_many_requests","message":"body-token-secret"}`)),
			Request: req,
		}
	}

	_ = mkErr(newResponse("7"))
	if got := output.String(); got != "" {
		t.Fatalf("valid Retry-After log = %q, want no warning", got)
	}

	const rawRetryAfter = "not-seconds-header-secret"
	_ = mkErr(newResponse(rawRetryAfter))
	const warning = "couldn't parse retry-after header"
	if got := strings.Count(output.String(), warning); got != 1 {
		t.Fatalf("invalid Retry-After warning count = %d, want 1; log = %q", got, output.String())
	}
	for _, secret := range []string{rawRetryAfter, "request-key-secret", "response-token-secret", "body-token-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("Retry-After warning exposed %q", secret)
		}
	}
}

func TestMakeRequestBodyDrainWarning(t *testing.T) {
	if os.Getenv("BLAZER_BODY_DRAIN_LOG_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestMakeRequestBodyDrainWarning$")
		command.Env = append(os.Environ(), "B2_LOG_LEVEL=1", "BLAZER_BODY_DRAIN_LOG_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("body drain log test failed: %v\n%s", err, output)
		}
		return
	}
	var output bytes.Buffer
	log.SetOutput(&output)
	newOptions := func(body io.ReadCloser) *b2Options {
		return &b2Options{transport: logRoundTrip(func(req *http.Request) (*http.Response, error) {
			return &http.Response{Request: req, StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
		})}
	}
	const method = "b2_delete_file_version"
	const uri = "https://example.invalid/b2api/v4/b2_delete_file_version"
	headers := map[string]string{"Authorization": "request-key-secret"}

	if err := newOptions(io.NopCloser(strings.NewReader(`{}`))).makeRequest(context.Background(), method, http.MethodPost, uri, nil, nil, headers, nil); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "" {
		t.Fatalf("successful body drain log = %q, want no warning", got)
	}

	output.Reset()
	if err := newOptions(failingLogBody{}).makeRequest(context.Background(), method, http.MethodPost, uri, nil, nil, headers, nil); err != nil {
		t.Fatal(err)
	}
	const warning = "b2_delete_file_version: couldn't read response body"
	if got := strings.Count(output.String(), warning); got != 1 {
		t.Fatalf("failed body drain warning count = %d, want 1; log = %q", got, output.String())
	}
	for _, secret := range []string{"request-key-secret", "body-read-secret"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("body drain warning exposed %q", secret)
		}
	}
}
