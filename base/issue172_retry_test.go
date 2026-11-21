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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"
	"testing/iotest"
)

var issue172Writes = []string{
	"b2_create_key", "b2_create_bucket", "b2_start_large_file", "b2_finish_large_file",
	"b2_hide_file", "b2_copy_file", "b2_delete_file_version",
}

type issue172SensitiveCause string

func (e issue172SensitiveCause) Error() string { return string(e) }

func TestIssue172RetainedCauseStaysRedactedWhenFormatted(t *testing.T) {
	const id, key = "private-test-file-id", "private-test-customer-key"
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/download?fileId="+id, nil)
	req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
	req.Header.Set("X-Bz-Server-Side-Encryption-Customer-Key", key)
	cause := issue172SensitiveCause("failed " + req.URL.String() + " with " + key)
	_, err := makeNetRequest(req.Context(), req, authSnapshotTransport(func(*http.Request) (*http.Response, error) { return nil, cause }))
	if !errors.Is(err, cause) {
		t.Fatal("original transport cause was lost")
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		got := fmt.Sprintf(verb, err)
		for _, secret := range []string{id, key, req.URL.String()} {
			if strings.Contains(got, secret) || strings.Contains(got, fmt.Sprintf("%x", secret)) {
				t.Errorf("%s exposed the retained transport cause", verb)
			}
		}
	}
}

func TestIssue172ResponseBodyLoss(t *testing.T) {
	for _, method := range issue172Writes {
		t.Run(method, func(t *testing.T) {
			opts := &b2Options{transport: authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}, nil
			})}
			var reply map[string]any
			err := opts.makeRequest(context.Background(), method, http.MethodPost, "https://example.invalid", nil, &reply, nil, nil)
			if Action(err) != OutcomeUnknown || MaxRetries(err) != 0 || !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("response-body error=%v action=%v retries=%d; want OutcomeUnknown and original cause", err, Action(err), MaxRetries(err))
			}
		})
	}
	for _, method := range []string{"b2_list_file_names", "b2_get_upload_url", "b2_upload_file", "b2_upload_part"} {
		t.Run(method, func(t *testing.T) {
			opts := &b2Options{transport: authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req, Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}, nil
			})}
			var reply map[string]any
			err := opts.makeRequest(context.Background(), method, http.MethodPost, "https://example.invalid", nil, &reply, nil, nil)
			if err != io.ErrUnexpectedEOF || Action(err) != Punt || errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("response-body error=%v action=%v unknown=%t; want raw unexpected EOF, Punt, false", err, Action(err), errors.Is(err, ErrOutcomeUnknown))
			}
		})
	}
}

func TestIssue172SentRequestEvidence(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, &net.OpError{Op: "dial", Err: errors.New("later connection refused")}} {
		t.Run(cause.Error(), func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
			req.Header.Set("X-Blazer-Method", "b2_create_key")
			_, err := makeNetRequest(req.Context(), req, authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
				if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteHeaders != nil {
					trace.WroteHeaders()
				}
				return nil, cause
			}))
			if Action(err) != OutcomeUnknown || MaxRetries(err) != 0 || !errors.Is(err, cause) || !errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("post-send error=%v action=%v retries=%d; want OutcomeUnknown and retained cause", err, Action(err), MaxRetries(err))
			}
		})
	}
}

func TestIssue172CancellationControls(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run("b2_create_key/pre_send/"+cause.Error(), func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Blazer-Method", "b2_create_key")
			_, err = makeNetRequest(req.Context(), req, authSnapshotTransport(func(*http.Request) (*http.Response, error) {
				return nil, cause // No WroteHeaders or WroteRequest: nothing was sent.
			}))
			if err != cause || Action(err) != Punt || errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("pre-send error=%v action=%v unknown=%t; want raw %v, Punt, false", err, Action(err), errors.Is(err, ErrOutcomeUnknown), cause)
			}
		})
	}
	for _, method := range []string{"b2_download_file_by_name", "b2_upload_file", "b2_upload_part"} {
		t.Run(method, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
			req.Header.Set("X-Blazer-Method", method)
			_, err := makeNetRequest(req.Context(), req, authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
				if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.WroteHeaders != nil {
					trace.WroteHeaders()
				}
				return nil, context.DeadlineExceeded
			}))
			if err != context.DeadlineExceeded || Action(err) != Punt {
				t.Errorf("deadline error=%v action=%v, want unchanged context deadline and Punt", err, Action(err))
			}
		})
	}
}

func issue172TransportError(t *testing.T, method string, cause error) error {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Blazer-Method", method)
	_, err = makeNetRequest(req.Context(), req, authSnapshotTransport(func(*http.Request) (*http.Response, error) { return nil, cause }))
	return err
}

func TestIssue172WritesDoNotRetryAmbiguousReplies(t *testing.T) {
	for _, method := range issue172Writes {
		for _, status := range []int{408, 500, 502, 504} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				err := b2err{method: method, code: status, retry: 7, msg: "lost reply"}
				if got := Action(err); got == Retry {
					t.Errorf("Action = Retry, want outcome unknown")
				}
				if got := MaxRetries(err); got != 0 {
					t.Errorf("MaxRetries = %d, want 0", got)
				}
			})
		}
	}
}

func TestIssue172TransportCauseIsRetained(t *testing.T) {
	for _, method := range issue172Writes {
		t.Run(method, func(t *testing.T) {
			err := issue172TransportError(t, method, fmt.Errorf("lost response: %w", io.EOF))
			if !errors.Is(err, io.EOF) {
				t.Errorf("errors.Is(%v, io.EOF) = false, want true", err)
			}
			if got := Action(err); got == Retry {
				t.Errorf("Action = Retry, want outcome unknown")
			}
			if got := MaxRetries(err); got != 0 {
				t.Errorf("MaxRetries = %d, want 0", got)
			}
		})
	}
}

func TestIssue172TransportErrorHasNoZeroStatus(t *testing.T) {
	err := b2err{method: "b2_create_key", msg: "EOF"}
	if got := err.Error(); strings.Contains(got, ": 0:") || !strings.Contains(got, "b2_create_key") || !strings.Contains(got, "EOF") {
		t.Errorf("Error = %q, want method and cause without a zero HTTP status", got)
	}
}

func TestIssue172OutcomeUnknownAPI(t *testing.T) {
	for _, method := range issue172Writes {
		for _, status := range []int{408, 500, 502, 504} {
			for _, retryAfter := range []int{0, 7} {
				t.Run(fmt.Sprintf("%s/%d/retry-after=%d", method, status, retryAfter), func(t *testing.T) {
					err := fmt.Errorf("context: %w", b2err{method: method, code: status, retry: retryAfter, msg: "scripted"})
					if got := Action(err); got != OutcomeUnknown {
						t.Errorf("Action = %v, want OutcomeUnknown", got)
					}
					if got := MaxRetries(err); got != 0 {
						t.Errorf("MaxRetries = %d, want 0", got)
					}
					if !errors.Is(err, ErrOutcomeUnknown) {
						t.Errorf("errors.Is(%v, ErrOutcomeUnknown) = false", err)
					}
					if !strings.Contains(err.Error(), "outcome unknown") {
						t.Errorf("error does not explain the uncertain outcome: %v", err)
					}
				})
			}
		}
		for _, cause := range []error{io.EOF, io.ErrUnexpectedEOF, &net.OpError{Op: "read", Err: errors.New("reset")}, &net.OpError{Op: "write", Err: errors.New("reset")}} {
			t.Run(method+"/"+cause.Error(), func(t *testing.T) {
				err := issue172TransportError(t, method, cause)
				if Action(err) != OutcomeUnknown || MaxRetries(err) != 0 || !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, cause) {
					t.Errorf("error=%v action=%v retries=%d; want OutcomeUnknown, zero retries, marker and original cause", err, Action(err), MaxRetries(err))
				}
			})
		}
	}
}

func TestIssue172RetryControls(t *testing.T) {
	for _, method := range issue172Writes {
		for _, status := range []int{429, 503, 401, 408} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				err := b2err{method: method, code: status}
				want, wantRetries, wantUnknown := Retry, uint(5), false
				switch status {
				case 401:
					want = ReAuthenticate
				case 408:
					want, wantRetries, wantUnknown = OutcomeUnknown, 0, true
				}
				if Action(err) != want || MaxRetries(err) != wantRetries || errors.Is(err, ErrOutcomeUnknown) != wantUnknown {
					t.Errorf("action=%v retries=%d unknown=%t; want %v, %d, %t", Action(err), MaxRetries(err), errors.Is(err, ErrOutcomeUnknown), want, wantRetries, wantUnknown)
				}
			})
		}
		for _, cause := range []error{&net.OpError{Op: "dial", Err: errors.New("refused")}, &net.DNSError{Err: "not found", Name: "example.invalid"}} {
			t.Run(method+"/"+cause.Error(), func(t *testing.T) {
				err := issue172TransportError(t, method, fmt.Errorf("network: %w", cause))
				if Action(err) != Retry || MaxRetries(err) != 5 || errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, cause) {
					t.Errorf("error=%v action=%v retries=%d; want Retry, 5 and original cause without outcome-unknown marker", err, Action(err), MaxRetries(err))
				}
			})
		}
		t.Run(method+"/TLS handshake", func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
			req.Header.Set("X-Blazer-Method", method)
			cause := errors.New("handshake failed before HTTP")
			_, err := makeNetRequest(req.Context(), req, authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
				if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.TLSHandshakeDone != nil {
					trace.TLSHandshakeDone(tls.ConnectionState{}, cause)
				}
				return nil, cause
			}))
			if Action(err) != Retry || !errors.Is(err, cause) || errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("handshake error=%v action=%v; want Retry and retained cause", err, Action(err))
			}
		})
	}
	for _, method := range []string{"b2_upload_file", "b2_upload_part"} {
		t.Run(method+"/408", func(t *testing.T) {
			err := b2err{method: method, code: http.StatusRequestTimeout}
			if Action(err) != AttemptNewUpload || errors.Is(err, ErrOutcomeUnknown) {
				t.Errorf("action=%v unknown=%t; want AttemptNewUpload, false", Action(err), errors.Is(err, ErrOutcomeUnknown))
			}
		})
	}
	for _, method := range []string{"b2_list_file_names", "b2_get_file_info", "b2_download_file_by_name", "b2_download_file_by_id"} {
		for _, status := range []int{408, 500, 502, 503, 504} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				err := b2err{method: method, code: status}
				if Action(err) != Retry || errors.Is(err, ErrOutcomeUnknown) {
					t.Errorf("action=%v unknown=%t; want Retry, false", Action(err), errors.Is(err, ErrOutcomeUnknown))
				}
			})
		}
	}
}
