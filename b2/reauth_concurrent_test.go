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
