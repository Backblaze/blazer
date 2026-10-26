package base

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type authSnapshotTransport func(*http.Request) (*http.Response, error)

func (f authSnapshotTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAuthorizationSnapshotDoesNotHoldLockAcrossNetwork(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"buckets":[]}`)), Request: req}, nil
	})
	options := &b2Options{transport: transport}
	account := &B2{accountID: "old", authToken: "old", apiURI: "https://old.invalid", opts: options}
	fresh := &B2{accountID: "new", authToken: "new", apiURI: "https://new.invalid", opts: options}
	requestDone := make(chan error, 1)
	go func() {
		_, err := account.ListBuckets(context.Background(), "")
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached the transport")
	}
	updateDone := make(chan struct{})
	go func() {
		account.Update(fresh)
		close(updateDone)
	}()
	select {
	case <-updateDone:
	case <-time.After(3 * time.Second):
		t.Error("authorization update waited for network I/O")
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	<-updateDone
}

func TestAuthorizationTokenRedactionCoversCompactResponse(t *testing.T) {
	for _, response := range []string{
		`{"authorizationToken":"test-secret-token"}`,
		`{"authorizationToken": "test-secret-token"}`,
		`{"authorization\u0054oken":"test-secret-token"}`,
	} {
		redacted := redactResponse("b2_authorize_account", []byte(response))
		if strings.Contains(redacted, "test-secret-token") {
			t.Errorf("authorization response still contains a token after redaction: %q", redacted)
		}
	}
	if redacted := redactResponse("b2_get_upload_url", []byte(`{"authorizationToken":"test-secret-token"}`)); strings.Contains(redacted, "test-secret-token") {
		t.Errorf("upload URL response still contains a token after redaction: %q", redacted)
	}
}

func TestConcurrentUpdateUsesConsistentAuthorizationSnapshot(t *testing.T) {
	transport := authSnapshotTransport(func(req *http.Request) (*http.Response, error) {
		var body struct {
			AccountID string `json:"accountId"`
			BucketID  string `json:"bucketId"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if want := map[string]string{"old.invalid": "old", "new.invalid": "new"}[req.URL.Host]; want == "" || req.Header.Get("Authorization") != want || body.AccountID != want || body.BucketID != want+"-bucket" {
			t.Errorf("mixed authorization snapshot: host=%q token=%q account=%q bucket=%q", req.URL.Host, req.Header.Get("Authorization"), body.AccountID, body.BucketID)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"buckets":[]}`)), Request: req}, nil
	})
	options := &b2Options{transport: transport}
	oldState := &B2{accountID: "old", authToken: "old", apiURI: "https://old.invalid", buckets: []string{"old-bucket"}, opts: options}
	old := &B2{accountID: "old", authToken: "old", apiURI: "https://old.invalid", buckets: []string{"old-bucket"}, opts: options}
	newState := &B2{accountID: "new", authToken: "new", apiURI: "https://new.invalid", s3URI: "https://s3.invalid", buckets: []string{"new-bucket"}, opts: options}

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			old.Update(oldState)
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
