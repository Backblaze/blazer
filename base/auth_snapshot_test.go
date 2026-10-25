package base

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type authSnapshotTransport func(*http.Request) (*http.Response, error)

func (f authSnapshotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestConcurrentUpdateUsesConsistentAuthorizationSnapshot(t *testing.T) {
	transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
		var body struct {
			AccountID string `json:"accountId"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if want := map[string]string{"old.invalid": "old", "new.invalid": "new"}[req.URL.Host]; want == "" || req.Header.Get("Authorization") != want || body.AccountID != want {
			t.Errorf("mixed authorization snapshot: host=%q token=%q account=%q", req.URL.Host, req.Header.Get("Authorization"), body.AccountID)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"buckets":[]}`)), Request: req}, nil
	})
	options := &b2Options{transport: transport}
	old := &B2{accountID: "old", authToken: "old", apiURI: "https://old.invalid", opts: options}
	newState := &B2{accountID: "new", authToken: "new", apiURI: "https://new.invalid", s3URI: "https://s3.invalid", opts: options}

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			old.Update(newState)
		}
	}()
	for i := 0; i < 4; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 250; j++ {
				if _, err := old.ListBuckets(context.Background(), ""); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if got := old.snapshot().s3URI; got != "https://s3.invalid" {
		t.Errorf("S3 endpoint after update = %q", got)
	}
}
