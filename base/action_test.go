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
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset")
}

func TestActionConnectionFaultUpload(t *testing.T) {
	tests := []struct {
		name string
		err  b2err
		want ErrAction
	}{
		{
			name: "upload file",
			err:  b2err{method: "b2_upload_file", code: 0, retry: 1},
			want: AttemptNewUpload,
		},
		{
			name: "upload part",
			err:  b2err{method: "b2_upload_part", code: 0, retry: 1},
			want: AttemptNewUpload,
		},
		{
			name: "download",
			err:  b2err{method: "b2_download_file_by_name", retry: 1},
			want: Retry,
		},
		{
			name: "no method",
			err:  b2err{retry: 1},
			want: Retry,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Action(test.err); got != test.want {
				t.Errorf("Action(%#v) = %v; want %v", test.err, got, test.want)
			}
		})
	}
}

func TestActionTransportFaultUpload(t *testing.T) {
	for _, method := range []string{"b2_upload_file", "b2_upload_part"} {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://example.com/upload", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Blazer-Method", method)
			resp, err := makeNetRequest(req.Context(), req, errorTransport{})
			if resp != nil {
				t.Fatalf("makeNetRequest returned response on transport fault: %v", resp)
			}
			if got := Action(err); got != AttemptNewUpload {
				t.Errorf("Action(transport fault) = %v; want AttemptNewUpload", got)
			}
			if code, _ := Code(err); code != 0 {
				t.Errorf("Code(transport fault) = %d; want 0", code)
			}
		})
	}
}

func TestActionUploadRetryAfter(t *testing.T) {
	const retryAfter = 7
	for _, method := range []string{"b2_upload_file", "b2_upload_part"} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(method+"/"+http.StatusText(status), func(t *testing.T) {
				req, err := http.NewRequest(http.MethodPost, "https://example.com/upload", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-Blazer-Method", method)
				resp := &http.Response{
					StatusCode: status,
					Header:     http.Header{"Retry-After": []string{"7"}},
					Body:       io.NopCloser(strings.NewReader(`{"code":"retry","message":"try later"}`)),
					Request:    req,
				}
				err = mkErr(resp)
				if got := Action(err); got != Retry {
					t.Errorf("Action(%d with Retry-After) = %v; want Retry on same URL", status, got)
				}
				if got := Backoff(err); got != retryAfter*time.Second {
					t.Errorf("Backoff(%d with Retry-After) = %v; want %v", status, got, retryAfter*time.Second)
				}
				if code, _ := Code(err); code != status {
					t.Errorf("Code(%d response) = %d", status, code)
				}
			})
		}
	}
}
