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
	"time"
)

func TestFindingC34_StatusBeforeAnyRequestDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := &InMemoryAccountInfo{}
	store.Set(sampleAuthData())
	var requests atomic.Int32
	transport := bucketInfoRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"buckets":[]}`)),
			Request:    req,
		}, nil
	})
	client, err := NewClient(ctx, "key-id", "synthetic-key", WithAccountInfo(store), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("restoring the saved session sent %d requests, want 0", got)
	}

	status := client.Status()
	if status == nil || len(status.Readers) != 0 || len(status.Writers) != 0 {
		t.Fatalf("initial status = %+v, want an empty client", status)
	}
	for _, duration := range []time.Duration{time.Minute, 5 * time.Minute, time.Hour, 0} {
		methods, ok := status.RPCs[duration]
		if !ok || len(methods) != 0 {
			t.Errorf("initial RPCs[%v] = %v (present=%t), want an empty window", duration, methods, ok)
		}
		if counts := methods.CountByMethod(); len(counts) != 0 {
			t.Errorf("initial RPC counts[%v] = %v, want empty", duration, counts)
		}
	}
	if table := status.table(); len(table) != 0 {
		t.Errorf("initial status table = %v, want empty", table)
	}
	if methods := funcMap["methods"].(func(*StatusInfo) []string)(status); len(methods) != 0 {
		t.Errorf("initial status-page methods = %v, want empty", methods)
	}
	response := httptest.NewRecorder()
	client.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "b2 client status") || !strings.Contains(response.Body.String(), "</html>") {
		t.Fatalf("status page = %d %q, want the status page", response.Code, response.Body.String())
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("status inspection sent %d requests, want 0", got)
	}

	if _, err := client.ListBuckets(ctx); err != nil {
		t.Fatalf("list buckets after status: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests after ListBuckets = %d, want 1", got)
	}
	for duration, methods := range client.Status().RPCs {
		if got := methods.CountByMethod()["b2_list_buckets"]; got != 1 {
			t.Errorf("RPCs[%v] list-buckets count = %d, want 1", duration, got)
		}
	}
}

func TestFindingC34_MethodCountersStartWithTypedEmptyWindow(t *testing.T) {
	for _, tc := range []struct {
		duration, resolution time.Duration
	}{
		{time.Minute, time.Second},
		{5 * time.Minute, time.Second},
		{time.Hour, time.Minute},
		{0, 0},
	} {
		t.Run(tc.duration.String(), func(t *testing.T) {
			counter := newMethodCounter(tc.duration, tc.resolution)
			for read := 0; read < 2; read++ {
				value := counter.w.Reduce()
				methods, ok := value.([]method)
				if !ok || len(methods) != 0 {
					t.Fatalf("initial window Reduce() = %T %v, want an empty []method", value, value)
				}
				if counts := counter.retrieve().CountByMethod(); len(counts) != 0 {
					t.Fatalf("initial counter counts = %v, want empty", counts)
				}
			}
			counter.record(method{name: "b2_list_buckets", status: http.StatusOK})
			counter.record(method{name: "b2_list_buckets", status: http.StatusOK})
			methods := counter.retrieve()
			counts := methods.CountByMethod()
			if len(methods) != 2 || len(counts) != 1 || counts["b2_list_buckets"] != 2 {
				t.Fatalf("recorded methods = %v, counts = %v, want exactly two list-buckets RPCs", methods, counts)
			}
		})
	}
}
