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
	}
	response := httptest.NewRecorder()
	client.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "b2 client status") {
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
