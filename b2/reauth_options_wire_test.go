package b2

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Like TestConcurrent401ReauthorizationWire, force reauth through a public
// call; check the options' HTTP effects instead of comparing internal structs.
func TestReauthPreservesOptionsWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := b2wiretest.New(t)
	auth := fmt.Sprintf(updateAuthJSON, srv.URL())
	srv.ReplySequence("b2_authorize_account",
		b2wiretest.Response{Body: auth},
		b2wiretest.Response{Body: strings.Replace(auth, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)},
	)
	srv.ReplySequence("b2_list_buckets",
		b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`},
		b2wiretest.Response{Body: `{"buckets":[]}`},
	)
	recorder := &recordingTransport{next: http.DefaultTransport}
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(recorder),
		UserAgent("first-agent"), UserAgent("second-agent"), FailSomeUploads(), ExpireSomeAuthTokens(), ForceCapExceeded())
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 0 {
		t.Fatalf("ListBuckets = %v, want empty", buckets)
	}
	requests := srv.Requests()
	wantCalls := []string{"b2_authorize_account", "b2_list_buckets", "b2_authorize_account", "b2_list_buckets"}
	if len(requests) != len(wantCalls) {
		t.Fatalf("HTTP calls = %d, want %d", len(requests), len(wantCalls))
	}
	recorder.mu.Lock()
	transportCalls := len(recorder.requests)
	recorder.mu.Unlock()
	if transportCalls != 4 {
		t.Errorf("configured transport calls = %d, want 4", transportCalls)
	}
	for i, request := range requests {
		method := http.MethodPost
		if i%2 == 0 {
			method = http.MethodGet
		}
		if request.Method != method || request.Path != "/b2api/v4/"+wantCalls[i] {
			t.Errorf("request %d = %s %s, want %s %s", i, request.Method, request.Path, method, wantCalls[i])
		}
		if got := request.Header.Get("User-Agent"); !strings.HasPrefix(got, "second-agent first-agent blazer/") {
			t.Errorf("request %d User-Agent = %q, want both custom agents before blazer/", i, got)
		}
		wantModes := []string{"expire_some_account_authorization_tokens", "fail_some_uploads", "force_cap_exceeded"}
		if got := normalizedTestModes(request.Header.Values("X-Bz-Test-Mode")); !reflect.DeepEqual(got, wantModes) {
			t.Errorf("request %d test modes = %q, want %q", i, got, wantModes)
		}
	}
	lists := srv.Calls("b2_list_buckets")
	if len(lists) != 2 {
		t.Fatalf("list calls = %d, want 2", len(lists))
	}
	for i, token := range []string{"t", "fresh"} {
		if got := lists[i].Header.Get("Authorization"); got != token {
			t.Errorf("list %d token = %q, want %q", i, got, token)
		}
		b2wiretest.AssertJSON(t, lists[i], "accountId", "a")
	}
}

func normalizedTestModes(values []string) []string {
	var modes []string
	for _, value := range values {
		for _, mode := range strings.Split(value, ",") {
			modes = append(modes, strings.TrimSpace(mode))
		}
	}
	sort.Strings(modes)
	return modes
}
