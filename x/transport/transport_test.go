package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithFailuresPassesThrough(t *testing.T) {
	srv := transportTestServer(t)
	resp := transportRequest(t, &http.Client{Transport: WithFailures(nil)}, context.Background(), srv.URL+"/ok", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestFailureRateAndPathMatch(t *testing.T) {
	srv := transportTestServer(t)
	tests := []struct {
		name string
		rt   http.RoundTripper
		path string
		want int
	}{
		{"rate zero passes through", WithFailures(nil, FailureRate(0), Response(http.StatusServiceUnavailable)), "/ok", http.StatusOK},
		{"rate one always fails", WithFailures(nil, FailureRate(1), Response(http.StatusServiceUnavailable)), "/ok", http.StatusServiceUnavailable},
		{"path mismatch passes through", WithFailures(nil, FailureRate(1), MatchPathSubstring("match"), Response(http.StatusServiceUnavailable)), "/other", http.StatusOK},
		{"path match fails", WithFailures(nil, FailureRate(1), MatchPathSubstring("match"), Response(http.StatusServiceUnavailable)), "/match", http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := transportRequest(t, &http.Client{Transport: tc.rt}, context.Background(), srv.URL+tc.path, nil)
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestResponseAndBody(t *testing.T) {
	client := &http.Client{Transport: WithFailures(nil, FailureRate(1), Response(http.StatusServiceUnavailable), Body("retry later"))}
	resp := transportRequest(t, client, context.Background(), "http://example.invalid", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), "retry later"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestStallWaitsForDurationOrContext(t *testing.T) {
	srv := transportTestServer(t)
	const delay = 20 * time.Millisecond
	t.Run("duration", func(t *testing.T) {
		client := &http.Client{Transport: WithFailures(nil, FailureRate(1), Stall(delay))}
		start := time.Now()
		resp := transportRequest(t, client, context.Background(), srv.URL+"/ok", nil)
		if elapsed := time.Since(start); elapsed < delay {
			t.Errorf("returned after %v, want at least %v", elapsed, delay)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
	t.Run("context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), delay)
		defer cancel()
		const stall = time.Second
		client := &http.Client{Transport: WithFailures(nil, FailureRate(1), Stall(stall))}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/ok", nil)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err = client.Do(request)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("error = %v, want context deadline exceeded", err)
		}
		if elapsed := time.Since(start); elapsed >= stall/2 {
			t.Errorf("returned after %v, want before %v when the request context ends", elapsed, stall/2)
		}
	})
}

func TestAfterNBytesTriggersOnce(t *testing.T) {
	srv := transportTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var calls atomic.Int32
	triggered := make(chan struct{})
	client := &http.Client{Transport: WithFailures(nil, AfterNBytes(3, func() {
		if calls.Add(1) == 1 {
			close(triggered)
		}
	}))}
	resp := transportRequest(t, client, ctx, srv.URL+"/upload", strings.NewReader("abcdef"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	select {
	case <-triggered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("effect calls = %d, want 1", got)
	}
}

func transportTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func transportRequest(t *testing.T, client *http.Client, ctx context.Context, target string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	})
	return resp
}
