package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// This exercises the public client through real HTTP handling, including the
// request body, endpoint and token used before and after a simultaneous 401.
func TestConcurrent401ReauthorizationWire(t *testing.T) {
	const concurrent = 8
	type request struct {
		method, path, token, account string
	}
	var mu sync.Mutex
	var authCalls int
	var requests []request
	ready := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/b2_authorize_account") {
			mu.Lock()
			authCalls++
			call := authCalls
			mu.Unlock()
			body := fmt.Sprintf(updateAuthJSON, server.URL)
			if call > 1 {
				body = strings.Replace(body, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)
			}
			_, _ = fmt.Fprint(w, body)
			return
		}
		var body struct {
			AccountID string `json:"accountId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode list request: %v", err)
		}
		mu.Lock()
		requests = append(requests, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), body.AccountID})
		count := len(requests)
		if count == concurrent {
			close(ready)
		}
		mu.Unlock()
		if count <= concurrent {
			select {
			case <-ready:
			case <-r.Context().Done():
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"status":401,"code":"expired_auth_token","message":"expired"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"buckets":[]}`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := NewClient(ctx, "id", "secret", APIBase(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ListBuckets(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if authCalls != 2 || len(requests) != 2*concurrent {
		t.Fatalf("authorize/list requests = %d/%d, want 2/%d", authCalls, len(requests), 2*concurrent)
	}
	for i, req := range requests {
		wantToken := "t"
		if i >= concurrent {
			wantToken = "fresh"
		}
		if req.method != http.MethodPost || req.path != "/b2api/v4/b2_list_buckets" || req.token != wantToken || req.account != "a" {
			t.Errorf("request %d = %+v, want POST list with account a and %q token", i, req, wantToken)
		}
	}
}
