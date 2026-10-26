package b2

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// This transport holds the first eight list requests until each has used the
// original token, then returns 401 to all of them.
type concurrentReauthTransport struct {
	mu        sync.Mutex
	lists     int
	authCalls int
	badTokens int
	ready     chan struct{}
}

func (q *concurrentReauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status, body := 200, ""
	if strings.HasSuffix(req.URL.Path, "b2_authorize_account") {
		q.mu.Lock()
		q.authCalls++
		authCall := q.authCalls
		q.mu.Unlock()
		body = fmt.Sprintf(updateAuthJSON, "https://example.invalid")
		if authCall > 1 {
			body = strings.Replace(body, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)
		}
	} else {
		q.mu.Lock()
		q.lists++
		n := q.lists
		wantToken := "t"
		if n > 8 {
			wantToken = "fresh"
		}
		if req.Header.Get("Authorization") != wantToken {
			q.badTokens++
		}
		if n == 8 {
			close(q.ready)
		}
		q.mu.Unlock()
		if n <= 8 {
			select {
			case <-q.ready:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			status = 401
			body = `{"status":401,"code":"expired_auth_token","message":"expired"}`
		} else {
			body = `{"buckets":[]}`
		}
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestConcurrent401Reauthorization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt := &concurrentReauthTransport{ready: make(chan struct{})}
	client, err := NewClient(ctx, "id", "secret", APIBase("https://example.invalid"), Transport(rt))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ListBuckets(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	rt.mu.Lock()
	authCalls := rt.authCalls
	lists := rt.lists
	badTokens := rt.badTokens
	rt.mu.Unlock()
	if authCalls != 2 {
		t.Errorf("authorization calls = %d, want initial + one refresh", authCalls)
	}
	if lists != 16 {
		t.Errorf("list requests = %d, want eight initial + eight retries", lists)
	}
	if badTokens != 0 {
		t.Errorf("%d list requests used the wrong authorization token", badTokens)
	}
}

type stalledRefreshTransport func(*http.Request) (*http.Response, error)

func (f stalledRefreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRefreshWaiterHonorsItsContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var mu sync.Mutex
	authCalls := 0
	rt := stalledRefreshTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		if strings.HasSuffix(req.URL.Path, "b2_authorize_account") {
			authCalls++
			call := authCalls
			mu.Unlock()
			if call == 2 {
				close(entered)
				select {
				case <-release:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(updateAuthJSON, "https://example.invalid"))), Request: req}, nil
		}
		mu.Unlock()
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"buckets":[]}`)), Request: req}, nil
	})
	client, err := NewClient(context.Background(), "id", "secret", APIBase("https://example.invalid"), Transport(rt))
	if err != nil {
		t.Fatal(err)
	}
	root := client.backend.(*beRoot)
	generation := root.authGeneration()
	leader := make(chan error, 1)
	go func() { leader <- root.reauthorizeIfUnchanged(context.Background(), generation) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not reach transport")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	waiter := make(chan error, 1)
	go func() { waiter <- root.reauthorizeIfUnchanged(ctx, generation) }()
	select {
	case err := <-waiter:
		if err != context.DeadlineExceeded {
			t.Fatalf("waiter error = %v, want deadline exceeded", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled refresh waiter remained blocked")
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-leader; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 {
		t.Errorf("authorization calls = %d, want initial plus one refresh", authCalls)
	}
}
