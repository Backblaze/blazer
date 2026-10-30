package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type recordingBody struct {
	reader *strings.Reader
	reads  int
	closed bool
}

func newRecordingBody(content string) *recordingBody {
	return &recordingBody{reader: strings.NewReader(content)}
}

func (b *recordingBody) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (b *recordingBody) Close() error {
	b.closed = true
	return nil
}

func request(t *testing.T, ctx context.Context, body io.ReadCloser) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid/match", body)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func successResponse(req *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}
}

func TestTriggerConcurrentRoundTripsAreRaceFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt := WithFailures(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return successResponse(req), nil
	}), FailureRate(0), Trigger(ctx))

	const callers, calls = 16, 1_000
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range calls {
				resp, err := rt.RoundTrip(request(t, context.Background(), nil))
				if err != nil {
					t.Errorf("RoundTrip: %v", err)
					return
				}
				_ = resp.Body.Close()
			}
		}()
	}
	close(start)
	cancel()
	wg.Wait()
}

func TestTriggerBackgroundDoesNotLeakGoroutines(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	for range 32 {
		_ = WithFailures(nil, Trigger(context.Background()))
	}
	deadline := time.After(100 * time.Millisecond)
	for {
		if got := runtime.NumGoroutine(); got > before+4 {
			t.Fatalf("Trigger(context.Background()) added goroutines: before=%d after=%d", before, got)
		}
		select {
		case <-deadline:
			return
		default:
			runtime.Gosched()
		}
	}
}

func TestTriggerRaisesFailureRateAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int64
	rt := WithFailures(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return successResponse(req), nil
	}), FailureRate(0), Response(http.StatusServiceUnavailable), Trigger(ctx))
	resp, err := rt.RoundTrip(request(t, context.Background(), nil))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("before cancellation response = %#v, error = %v", resp, err)
	}
	cancel()
	deadline := time.After(5 * time.Second)
	for {
		resp, err = rt.RoundTrip(request(t, context.Background(), nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Trigger did not raise the failure rate after cancellation")
		default:
			runtime.Gosched()
		}
	}
	if got := calls.Load(); got < 1 {
		t.Fatalf("wrapped RoundTrip calls = %d, want the initial request before cancellation", got)
	}
}

func TestSyntheticResponseDrainsAndClosesRequestBody(t *testing.T) {
	body := newRecordingBody("request payload")
	req := request(t, context.Background(), body)
	const message = "synthetic payload"
	resp, err := WithFailures(roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("synthetic response reached wrapped RoundTripper")
		return nil, errors.New("unreachable")
	}), FailureRate(1), Response(http.StatusServiceUnavailable), Body(message)).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if !body.closed || body.reads == 0 {
		t.Fatalf("request body closed=%t reads=%d, want drained and closed", body.closed, body.reads)
	}
	if resp.Header == nil || resp.Status != "503 Service Unavailable" || resp.StatusCode != http.StatusServiceUnavailable || resp.Proto != "HTTP/1.1" || resp.ProtoMajor != 1 || resp.ProtoMinor != 1 || resp.ContentLength != int64(len(message)) || resp.Request != req {
		t.Fatalf("synthetic response = %#v", resp)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != message {
		t.Fatalf("synthetic response body = %q, want %q", content, message)
	}
}

func TestStallCancellationDoesNotCallWrappedRoundTripper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := newRecordingBody("request payload")
	var calls atomic.Int64
	rt := WithFailures(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return successResponse(req), nil
	}), FailureRate(1), Stall(time.Second))
	result := make(chan error, 1)
	go func() {
		_, err := rt.RoundTrip(request(t, ctx, body))
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stalled RoundTrip error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled RoundTrip did not return after cancellation")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("wrapped RoundTrip calls = %d, want 0 after cancellation", got)
	}
	if !body.closed {
		t.Fatal("cancelled stalled request body was not closed")
	}
}
