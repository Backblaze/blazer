package b2

import (
	"context"
	"errors"
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

// The first request holds an old-token 401 until a second request has
// completed the refresh. It must retry with the new token, not refresh again.
func TestLate401ReusesRefreshedGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stragglerEntered := make(chan struct{})
	releaseStraggler := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseStraggler) })
	var mu sync.Mutex
	authCalls, listCalls := 0, 0
	rt := stalledRefreshTransport(func(req *http.Request) (*http.Response, error) {
		status, body := 200, ""
		if strings.HasSuffix(req.URL.Path, "b2_authorize_account") {
			mu.Lock()
			authCalls++
			call := authCalls
			mu.Unlock()
			body = fmt.Sprintf(updateAuthJSON, "https://example.invalid")
			if call > 1 {
				body = strings.Replace(body, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)
			}
		} else {
			mu.Lock()
			listCalls++
			call := listCalls
			mu.Unlock()
			if call <= 2 {
				if got := req.Header.Get("Authorization"); got != "t" {
					return nil, fmt.Errorf("initial list token = %q, want old token", got)
				}
				if call == 1 {
					close(stragglerEntered)
					select {
					case <-releaseStraggler:
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
				}
				status = 401
				body = `{"status":401,"code":"expired_auth_token","message":"expired"}`
			} else {
				if got := req.Header.Get("Authorization"); got != "fresh" {
					return nil, fmt.Errorf("retry list token = %q, want refreshed token", got)
				}
				body = `{"buckets":[]}`
			}
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	client, err := NewClient(ctx, "id", "secret", APIBase("https://example.invalid"), Transport(rt))
	if err != nil {
		t.Fatal(err)
	}
	straggler := make(chan error, 1)
	go func() {
		_, err := client.ListBuckets(ctx)
		straggler <- err
	}()
	select {
	case <-stragglerEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := client.ListBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releaseStraggler) })
	if err := <-straggler; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 {
		t.Errorf("authorization calls = %d, want initial + one refresh", authCalls)
	}
	if listCalls != 4 {
		t.Errorf("list requests = %d, want two initial + two retries", listCalls)
	}
}

type countedAccountInfo struct {
	InMemoryAccountInfo
	mu     sync.Mutex
	sets   int
	clears int
}

func (a *countedAccountInfo) Set(data AuthData) {
	a.InMemoryAccountInfo.Set(data)
	a.mu.Lock()
	a.sets++
	a.mu.Unlock()
}

func (a *countedAccountInfo) Clear() {
	a.InMemoryAccountInfo.Clear()
	a.mu.Lock()
	a.clears++
	a.mu.Unlock()
}

func TestConcurrent401ReauthorizationWithAccountInfo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rt := &concurrentReauthTransport{ready: make(chan struct{})}
	store := &countedAccountInfo{}
	client, err := NewClient(ctx, "id", "secret", APIBase("https://example.invalid"), Transport(rt), WithAccountInfo(store))
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
	authCalls, badTokens := rt.authCalls, rt.badTokens
	rt.mu.Unlock()
	store.mu.Lock()
	sets, clears := store.sets, store.clears
	store.mu.Unlock()
	data, ok := store.AuthData()
	if authCalls != 2 || badTokens != 0 {
		t.Errorf("authorization calls = %d, bad tokens = %d; want initial + one refresh, no bad tokens", authCalls, badTokens)
	}
	if sets != 2 || clears != 1 {
		t.Errorf("account info sets = %d, clears = %d; want initial + one refresh save and one clear", sets, clears)
	}
	if !ok || data.AuthToken != "fresh" {
		t.Errorf("stored authorization token = %q, present = %t; want refreshed token", data.AuthToken, ok)
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

func TestCancelledRefreshLeaderHandsOff(t *testing.T) {
	for _, waiters := range []int{1, 8} {
		t.Run(fmt.Sprintf("waiters=%d", waiters), func(t *testing.T) {
			entered := make(chan struct{})
			var mu sync.Mutex
			authCalls := 0
			rt := stalledRefreshTransport(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "b2_authorize_account") {
					mu.Lock()
					authCalls++
					call := authCalls
					mu.Unlock()
					if call == 2 {
						close(entered)
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(updateAuthJSON, "https://example.invalid"))), Request: req}, nil
				}
				return nil, fmt.Errorf("unexpected request %s", req.URL)
			})
			client, err := NewClient(context.Background(), "id", "secret", APIBase("https://example.invalid"), Transport(rt))
			if err != nil {
				t.Fatal(err)
			}
			root := client.backend.(*beRoot)
			generation := root.authGeneration()
			leaderCtx, cancel := context.WithCancel(context.Background())
			leader := make(chan error, 1)
			go func() { leader <- root.reauthorizeIfUnchanged(leaderCtx, generation) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("leader did not start refresh")
			}
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			results := make(chan error, waiters)
			for i := 0; i < waiters; i++ {
				go func() { results <- root.reauthorizeIfUnchanged(ctx, generation) }()
			}
			cancel()
			if err := <-leader; !errors.Is(err, context.Canceled) {
				t.Fatalf("leader error = %v, want cancellation", err)
			}
			for i := 0; i < waiters; i++ {
				if err := <-results; err != nil {
					t.Fatalf("waiter %d: %v", i, err)
				}
			}
			mu.Lock()
			got := authCalls
			mu.Unlock()
			if got != 3 {
				t.Fatalf("authorization calls = %d, want initial + cancelled + one takeover", got)
			}
		})
	}
}

func TestRefreshAuthorizationFailureReachesWaiters(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	rt := stalledRefreshTransport(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "b2_authorize_account") {
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		}
		select {
		case <-entered:
			<-release
			return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":403,"code":"bad_auth_token","message":"denied"}`)), Request: req}, nil
		default:
			close(entered)
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(updateAuthJSON, "https://example.invalid"))), Request: req}, nil
		}
	})
	client, err := NewClient(context.Background(), "id", "secret", APIBase("https://example.invalid"), Transport(rt))
	if err != nil {
		t.Fatal(err)
	}
	root := client.backend.(*beRoot)
	generation := root.authGeneration()
	results := make(chan error, 9)
	go func() { results <- root.reauthorizeIfUnchanged(context.Background(), generation) }()
	for i := 0; i < 8; i++ {
		go func() { results <- root.reauthorizeIfUnchanged(context.Background(), generation) }()
	}
	close(release)
	for i := 0; i < 9; i++ {
		if err := <-results; err == nil || !strings.Contains(err.Error(), "denied") {
			t.Fatalf("refresh %d error = %v, want authorization failure", i, err)
		}
	}
}
