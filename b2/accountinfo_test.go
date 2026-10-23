package b2

import (
	"context"
	"encoding/json"
	"fmt"
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
			w.mu.Unlock()
			if force {
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
	wire.force401 = true
	wire.mu.Unlock()
	if _, err := client.ListBuckets(ctx); err != nil {
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
