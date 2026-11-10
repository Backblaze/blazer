package b2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Fail at the storage boundary, while NewClient and its HTTP/retry path stay real.
type failingAccountInfoStore struct {
	InMemoryAccountInfo
	mu                        sync.Mutex
	readErr, setErr, clearErr error
	lastErr                   error
	sets, clears              int
}

func (s *failingAccountInfoStore) AuthData() (AuthData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = s.readErr
	return s.InMemoryAccountInfo.AuthData()
}

func (s *failingAccountInfoStore) Set(data AuthData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	s.lastErr = s.setErr
	if s.lastErr == nil {
		s.InMemoryAccountInfo.Set(data)
	}
}

func (s *failingAccountInfoStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clears++
	s.lastErr = s.clearErr
	if s.lastErr == nil {
		s.InMemoryAccountInfo.Clear()
	}
}

func (s *failingAccountInfoStore) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *failingAccountInfoStore) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets, s.clears
}

func TestAccountInfoClientPersistenceFailures(t *testing.T) {
	storageErr := errors.New("scripted storage failure")
	classifiedErr := fmt.Errorf("scripted storage failure: %w", ErrInvalidAccountInfo)
	for _, tc := range []struct {
		name                      string
		cached                    bool
		setErr, clearErr, wantErr error
		wantAuth, wantLists       int
		wantSets, wantClears      int
		wantToken                 string
	}{
		{name: "initial_set", setErr: storageErr, wantErr: storageErr, wantAuth: 1, wantSets: 1},
		{name: "initial_set_classified", setErr: classifiedErr, wantErr: classifiedErr, wantAuth: 1, wantSets: 1},
		{name: "refresh_set", cached: true, setErr: storageErr, wantErr: storageErr, wantAuth: 1, wantLists: 1, wantSets: 1, wantClears: 1},
		{name: "refresh_set_classified", cached: true, setErr: classifiedErr, wantErr: classifiedErr, wantAuth: 1, wantLists: 1, wantSets: 1, wantClears: 1},
		{name: "clear_after_401", cached: true, clearErr: storageErr, wantErr: storageErr, wantLists: 1, wantClears: 1, wantToken: "stale-token"},
		{name: "clear_after_401_classified", cached: true, clearErr: classifiedErr, wantErr: classifiedErr, wantLists: 1, wantClears: 1, wantToken: "stale-token"},
		{name: "clear_success", cached: true, wantAuth: 1, wantLists: 2, wantSets: 1, wantClears: 1, wantToken: "test-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			server := b2wiretest.New(t)
			store := &failingAccountInfoStore{setErr: tc.setErr, clearErr: tc.clearErr}
			var replies []b2wiretest.Response
			if tc.cached {
				data := sampleAuthData()
				data.APIURL, data.DownloadURL, data.S3URL = server.URL(), server.URL(), server.URL()
				data.AuthToken = "stale-token"
				store.InMemoryAccountInfo.Set(data)
				replies = append(replies, b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`})
				if tc.wantErr == nil {
					replies = append(replies, b2wiretest.Response{Body: `{"buckets":[]}`})
				}
			}
			server.StrictReplySequence("b2_list_buckets", replies...)
			client, err := NewClient(ctx, "key-id", "application-key-secret", APIBase(server.URL()), WithAccountInfo(store))
			if tc.cached {
				if err != nil {
					t.Fatalf("restore cached client: %v", err)
				}
				if got := len(server.Calls("b2_authorize_account")); got != 0 {
					t.Fatalf("cached client authorized before its 401: got %d calls, want 0", got)
				}
				_, err = client.ListBuckets(ctx)
			} else if client != nil {
				t.Error("NewClient returned a client after its session failed to persist")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("client error = %v, want %v", err, tc.wantErr)
			}
			if got := len(server.Calls("b2_authorize_account")); got != tc.wantAuth {
				t.Errorf("authorize calls = %d, want %d", got, tc.wantAuth)
			}
			calls := server.Calls("b2_list_buckets")
			if len(calls) != tc.wantLists {
				t.Errorf("list calls = %d, want %d", len(calls), tc.wantLists)
			}
			for i, call := range calls {
				want := "stale-token"
				if i > 0 {
					want = "test-token"
				}
				if got := call.Header.Get("Authorization"); got != want {
					t.Errorf("list request %d token = %q, want %q", i, got, want)
				}
			}
			if sets, clears := store.counts(); sets != tc.wantSets || clears != tc.wantClears {
				t.Errorf("store sets=%d clears=%d, want %d and %d", sets, clears, tc.wantSets, tc.wantClears)
			}
			data, ok := store.InMemoryAccountInfo.AuthData()
			if ok != (tc.wantToken != "") || data.AuthToken != tc.wantToken {
				t.Errorf("cached session: present=%t token=%q, want token=%q", ok, data.AuthToken, tc.wantToken)
			}
		})
	}
}

func TestAccountInfoClientReadErrorContract(t *testing.T) {
	classifiedErr := fmt.Errorf("custom cache data: %w", ErrInvalidAccountInfo)
	storageErr := &os.PathError{Op: "read", Path: "cached-session", Err: os.ErrPermission}
	unclassifiedErr := errors.New("corrupt session without a category")
	for _, tc := range []struct {
		name                 string
		cached               bool
		readErr, wantErr     error
		incomplete, mismatch bool
		wantAuth             int
	}{
		{name: "absent", wantAuth: 1},
		{name: "unusable", readErr: classifiedErr, wantAuth: 1},
		{name: "storage_error", readErr: storageErr, wantErr: storageErr},
		{name: "unclassified_error", readErr: unclassifiedErr, wantErr: unclassifiedErr},
		{name: "present_with_recoverable_error", cached: true, readErr: classifiedErr},
		{name: "incomplete_with_recoverable_error", cached: true, readErr: classifiedErr, incomplete: true, wantAuth: 1},
		{name: "mismatched_with_recoverable_error", cached: true, readErr: classifiedErr, mismatch: true, wantAuth: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := b2wiretest.New(t)
			server.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
			store := &failingAccountInfoStore{readErr: tc.readErr}
			if tc.cached {
				data := sampleAuthData()
				data.APIURL, data.DownloadURL, data.S3URL = server.URL(), server.URL(), server.URL()
				data.AuthToken = "stale-token"
				if tc.incomplete {
					data.AccountID = ""
				}
				if tc.mismatch {
					data.ApplicationKeyID = "other-key-id"
				}
				store.InMemoryAccountInfo.Set(data)
			}
			client, err := NewClient(t.Context(), "key-id", "application-key-secret", APIBase(server.URL()), WithAccountInfo(store))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewClient error = %v, want %v", err, tc.wantErr)
			}
			wantLists := 0
			if tc.wantErr == nil {
				if _, err := client.ListBuckets(t.Context()); err != nil {
					t.Fatal(err)
				}
				wantLists = 1
				wantToken := "test-token"
				if tc.wantAuth == 0 {
					wantToken = "stale-token"
				}
				if got := server.Last(t, "b2_list_buckets").Header.Get("Authorization"); got != wantToken {
					t.Errorf("list token = %q, want %q", got, wantToken)
				}
			} else if client != nil {
				t.Error("NewClient returned a client after its store failed to read")
			}
			if got := len(server.Calls("b2_authorize_account")); got != tc.wantAuth {
				t.Errorf("authorize calls = %d, want %d", got, tc.wantAuth)
			}
			if got := len(server.Calls("b2_list_buckets")); got != wantLists {
				t.Errorf("list calls = %d, want %d", got, wantLists)
			}
			if tc.readErr == storageErr {
				var pathErr *os.PathError
				if !errors.Is(err, os.ErrPermission) || !errors.As(err, &pathErr) || pathErr != storageErr {
					t.Errorf("storage error identity was lost: %v", err)
				}
			}
		})
	}
}
