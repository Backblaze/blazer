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
		var calls atomic.Int64
		body := newRecordingBody("request payload")
		rt := WithFailures(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return successResponse(req), nil
		}), FailureRate(1), Stall(time.Hour))
		result := make(chan error, 1)
		go func() {
			resp, err := rt.RoundTrip(request(t, ctx, body))
			if resp != nil {
				_ = resp.Body.Close()
			}
			result <- err
		}()
		select {
		case err := <-result:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want context.DeadlineExceeded", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("stalled RoundTrip did not stop after the request deadline")
		}
		if got := calls.Load(); got != 0 {
			t.Errorf("wrapped RoundTrip calls = %d, want 0 after the request deadline", got)
		}
		if !body.closed {
			t.Error("cancelled stalled request body was not closed")
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
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	resp = transportRequest(t, client, ctx, srv.URL+"/upload", strings.NewReader("abcdef"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("second status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("effect calls after second upload and both responses = %d, want 1", got)
	}
}

func TestAfterNBytesDoesNotTriggerForShortBody(t *testing.T) {
	srv := transportTestServer(t)
	var calls atomic.Int32
	client := &http.Client{Transport: WithFailures(nil, AfterNBytes(3, func() {
		calls.Add(1)
	}))}
	resp := transportRequest(t, client, context.Background(), srv.URL+"/upload", strings.NewReader("ab"))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("effect calls after short body = %d, want 0", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("response body = %q, want %q", body, "ok")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("effect calls after short body and response = %d, want 1", got)
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
