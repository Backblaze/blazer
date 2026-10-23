package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func sampleAuthData() AuthData {
	return AuthData{
		ApplicationKeyID: "key-id", AccountID: "account", AuthToken: "session-token",
		APIURL: "https://api.example", DownloadURL: "https://download.example",
		S3URL: "https://s3.example", AbsMinPartSize: 5_000_000,
		RecommendedPartSize: 100_000_000, AllowedBuckets: []string{"bucket"},
		AllowedPrefix: "prefix/", AllowedCapabilities: []string{"listBuckets"},
	}
}

func TestInMemoryAccountInfo(t *testing.T) {
	store := &InMemoryAccountInfo{}
	if _, ok := store.AuthData(); ok {
		t.Fatal("new store is populated")
	}
	data := sampleAuthData()
	store.Set(data)
	data.AllowedBuckets[0] = "modified"
	got, ok := store.AuthData()
	if !ok || got.AllowedBuckets[0] != "bucket" {
		t.Fatalf("stored data = %+v, ok=%t", got, ok)
	}
	got.AllowedCapabilities[0] = "modified"
	got, _ = store.AuthData()
	if got.AllowedCapabilities[0] != "listBuckets" {
		t.Fatal("returned data aliases stored slice")
	}
	store.Clear()
	if _, ok := store.AuthData(); ok {
		t.Fatal("Clear left a session")
	}
}

func TestAccountInfoFileRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "account.json")
	store := NewAccountInfoFile(path)
	want := sampleAuthData()
	store.Set(want)
	if err := store.Err(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(content) || strings.Contains(string(content), "application-key-secret") {
		t.Fatal("file is not valid JSON or contains the application key")
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 || parentInfo.Mode().Perm() != 0o700 {
		t.Fatalf("modes file=%#o parent=%#o", fileInfo.Mode().Perm(), parentInfo.Mode().Perm())
	}
	reloaded := NewAccountInfoFile(path)
	got, ok := reloaded.AuthData()
	if !ok || reloaded.Err() != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded = %+v, ok=%t, err=%v", got, ok, reloaded.Err())
	}
	reloaded.Clear()
	if err := reloaded.Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.AuthData(); ok {
		t.Fatal("Clear did not remove the persisted session")
	}
}

func TestAccountInfoFileRejectsPublicParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "account.json")
	store := NewAccountInfoFile(path)
	store.Set(sampleAuthData())
	if store.Err() == nil {
		t.Fatal("Set accepted a parent directory readable by other users")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file was created despite unsafe parent: %v", err)
	}
}

func TestAccountInfoConcurrency(t *testing.T) {
	stores := []AccountInfo{&InMemoryAccountInfo{}, NewAccountInfoFile(filepath.Join(t.TempDir(), "private", "account.json"))}
	for _, store := range stores {
		t.Run(fmt.Sprintf("%T", store), func(t *testing.T) {
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 20; j++ {
						store.Set(sampleAuthData())
						store.AuthData()
					}
				}()
			}
			wg.Wait()
			if f, ok := store.(*AccountInfoFile); ok && f.Err() != nil {
				t.Fatal(f.Err())
			}
		})
	}
}

func TestAccountInfoFileAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "account.json")
	first, second := NewAccountInfoFile(path), NewAccountInfoFile(path)
	first.Set(AuthData{AccountID: "first", AuthToken: "first"})
	if err := first.Err(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 3)
	for _, item := range []struct {
		store *AccountInfoFile
		value string
	}{{first, "first"}, {second, "second"}} {
		wg.Add(1)
		go func(store *AccountInfoFile, value string) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				store.Set(AuthData{AccountID: value, AuthToken: value})
				if err := store.Err(); err != nil {
					errors <- err
					return
				}
			}
		}(item.store, item.value)
	}
	for i := 0; i < 200; i++ {
		content, err := os.ReadFile(path)
		if err != nil {
			errors <- err
			break
		}
		var data AuthData
		if err := json.Unmarshal(content, &data); err != nil {
			errors <- fmt.Errorf("observed partial JSON: %w", err)
			break
		}
		if data.AccountID != data.AuthToken || (data.AccountID != "first" && data.AccountID != "second") {
			errors <- fmt.Errorf("observed mixed snapshot: %+v", data)
			break
		}
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".account-info-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestNewClientFallsBackFromUnavailableSession(t *testing.T) {
	for _, test := range []struct {
		name    string
		content []byte
		create  bool
	}{
		{name: "missing"},
		{name: "empty", content: []byte{}, create: true},
		{name: "malformed", content: []byte("{not-json"), create: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := newAccountInfoWire(t)
			path := filepath.Join(t.TempDir(), "private", "account.json")
			if test.create {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, test.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := NewAccountInfoFile(path)
			client, err := NewClient(context.Background(), "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(store))
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := client.ListBuckets(context.Background()); err != nil {
				t.Fatal(err)
			}
			wire.mu.Lock()
			authCalls := wire.authCalls
			wire.mu.Unlock()
			if authCalls != 1 {
				t.Fatalf("authorize calls=%d, want 1", authCalls)
			}
			data, ok := NewAccountInfoFile(path).AuthData()
			if !ok || data.AuthToken != "token-1" {
				t.Fatalf("replacement session=%+v ok=%t", data, ok)
			}
		})
	}
}

type accountInfoWire struct {
	server    *httptest.Server
	mu        sync.Mutex
	authCalls int
	listCalls int
	force401  bool
	onAuth    func(int)
}

func newAccountInfoWire(t *testing.T) *accountInfoWire {
	t.Helper()
	w := &accountInfoWire{}
	w.server = httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/b2_authorize_account"):
			w.mu.Lock()
			w.authCalls++
			call, onAuth := w.authCalls, w.onAuth
			w.mu.Unlock()
			if onAuth != nil {
				onAuth(call)
			}
			_, _ = fmt.Fprintf(out, `{"accountId":"account","authorizationToken":"token-%d","apiInfo":{"storageApi":{"absoluteMinimumPartSize":5000000,"recommendedPartSize":100000000,"apiUrl":%q,"downloadUrl":%q,"s3ApiUrl":%q,"allowed":{"buckets":null,"namePrefix":null},"capabilities":["listBuckets"]}}}`, call, w.server.URL, w.server.URL, w.server.URL)
		case strings.HasSuffix(req.URL.Path, "/b2_list_buckets"):
			w.mu.Lock()
			w.listCalls++
			force := w.force401
			w.force401 = false
			expected := fmt.Sprintf("token-%d", w.authCalls)
			w.mu.Unlock()
			if force || req.Header.Get("Authorization") != expected {
				out.WriteHeader(http.StatusUnauthorized)
				_, _ = out.Write([]byte(`{"status":401,"code":"expired_auth_token","message":"expired"}`))
				return
			}
			_, _ = out.Write([]byte(`{"buckets":[]}`))
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
			http.NotFound(out, req)
		}
	}))
	t.Cleanup(w.server.Close)
	return w
}

func TestNewClientDefaultsToInMemoryAccountInfo(t *testing.T) {
	wire := newAccountInfoWire(t)
	client, err := NewClient(context.Background(), "key-id", "application-key-secret", APIBase(wire.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.opts.accountInfo.(*InMemoryAccountInfo); !ok {
		t.Fatalf("default store = %T", client.opts.accountInfo)
	}
	if _, err := client.ListBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if wire.authCalls != 1 || wire.listCalls != 1 {
		t.Fatalf("calls auth=%d list=%d", wire.authCalls, wire.listCalls)
	}
}

func TestAccountInfoFileResumeAndClearOn401(t *testing.T) {
	wire := newAccountInfoWire(t)
	path := filepath.Join(t.TempDir(), "private", "account.json")
	store := NewAccountInfoFile(path)
	ctx := context.Background()
	if _, err := NewClient(ctx, "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(store)); err != nil {
		t.Fatal(err)
	}
	data, ok := store.AuthData()
	if !ok || data.RecommendedPartSize != 100_000_000 || data.AuthToken != "token-1" {
		t.Fatalf("first auth = %+v, ok=%t", data, ok)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "application-key-secret") {
		t.Fatal("application key secret was written to the session file")
	}
	wire.onAuth = func(call int) {
		if call == 2 {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("cached token was not cleared before reauth: %v", err)
			}
		}
	}
	client, err := NewClient(ctx, "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(NewAccountInfoFile(path)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	if wire.authCalls != 1 {
		t.Fatalf("fresh client reauthorized: %d calls", wire.authCalls)
	}
	wire.mu.Unlock()
	data.AuthToken = "invalid-token"
	corrupt := NewAccountInfoFile(path)
	corrupt.Set(data)
	if err := corrupt.Err(); err != nil {
		t.Fatal(err)
	}
	stale, err := NewClient(ctx, "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(NewAccountInfoFile(path)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stale.ListBuckets(ctx); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	authCalls, listCalls := wire.authCalls, wire.listCalls
	wire.mu.Unlock()
	if authCalls != 2 || listCalls != 3 {
		t.Fatalf("after 401 auth=%d list=%d", authCalls, listCalls)
	}
	data, ok = NewAccountInfoFile(path).AuthData()
	if !ok || data.AuthToken != "token-2" {
		t.Fatalf("replacement session = %+v, ok=%t", data, ok)
	}
}

type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

type recordingTransport struct {
	next     http.RoundTripper
	mu       sync.Mutex
	requests []recordedRequest
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		content, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = string(content)
		req.Body = io.NopCloser(strings.NewReader(body))
	}
	header := req.Header.Clone()
	header.Del("X-Blazer-Request-Id") // Process-global counter, not wire behavior controlled by AccountInfo.
	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{Method: req.Method, Path: req.URL.RequestURI(), Header: header, Body: body})
	r.mu.Unlock()
	return r.next.RoundTrip(req)
}

func TestDefaultAccountInfoWireMatchesExplicitInMemory(t *testing.T) {
	var snapshots [][]recordedRequest
	for _, explicit := range []bool{false, true} {
		wire := newAccountInfoWire(t)
		recorder := &recordingTransport{next: http.DefaultTransport}
		opts := []ClientOption{APIBase(wire.server.URL), Transport(recorder)}
		if explicit {
			opts = append(opts, WithAccountInfo(&InMemoryAccountInfo{}))
		}
		client, err := NewClient(context.Background(), "key-id", "application-key-secret", opts...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ListBuckets(context.Background()); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, recorder.requests)
	}
	for _, requests := range snapshots {
		if len(requests) != 2 || !strings.HasSuffix(requests[0].Path, "/b2_authorize_account") || !strings.HasSuffix(requests[1].Path, "/b2_list_buckets") {
			t.Fatalf("unexpected wire sequence: %+v", requests)
		}
	}
	if !reflect.DeepEqual(snapshots[0], snapshots[1]) {
		t.Fatalf("default and explicit in-memory wire requests differ:\ndefault=%+v\nexplicit=%+v", snapshots[0], snapshots[1])
	}
}

func TestAccountInfoSharedClientConcurrency(t *testing.T) {
	wire := newAccountInfoWire(t)
	store := NewAccountInfoFile(filepath.Join(t.TempDir(), "private", "account.json"))
	client, err := NewClient(context.Background(), "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(store))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				if _, err := client.ListBuckets(context.Background()); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	wire.mu.Lock()
	authCalls, listCalls := wire.authCalls, wire.listCalls
	wire.mu.Unlock()
	if authCalls != 1 || listCalls != 128 {
		t.Fatalf("auth=%d list=%d", authCalls, listCalls)
	}
}

func TestAccountInfoDoesNotReuseAnotherKeyID(t *testing.T) {
	wire := newAccountInfoWire(t)
	path := filepath.Join(t.TempDir(), "private", "account.json")
	ctx := context.Background()
	if _, err := NewClient(ctx, "key-id", "application-key-secret", APIBase(wire.server.URL), WithAccountInfo(NewAccountInfoFile(path))); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(ctx, "different-key-id", "different-secret", APIBase(wire.server.URL), WithAccountInfo(NewAccountInfoFile(path))); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if wire.authCalls != 2 {
		t.Fatalf("different key reused cached token: authorize calls=%d", wire.authCalls)
	}
}
