// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func clientOptionsWire(t *testing.T, opts ...ClientOption) (*b2wiretest.Server, *Client) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_key", http.StatusOK, `{"applicationKeyId":"id","applicationKey":"secret","keyName":"k","capabilities":["readFiles"],"expirationTimestamp":0}`)
	client, err := NewClient(context.Background(), "account", "key", append([]ClientOption{APIBase(srv.URL())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	return srv, client
}

// The client options must reach the base layer, so they show up on the
// authorize call as well as on later calls.
func TestClientTestModeOptionsReachTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []ClientOption
		want []string
	}{
		{"none", nil, nil},
		{"fail some uploads", []ClientOption{FailSomeUploads()}, []string{"fail_some_uploads"}},
		{"expire some tokens", []ClientOption{ExpireSomeAuthTokens()}, []string{"expire_some_account_authorization_tokens"}},
		{"force cap exceeded", []ClientOption{ForceCapExceeded()}, []string{"force_cap_exceeded"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := clientOptionsWire(t, tc.opts...)
			for _, call := range []string{"b2_authorize_account", "b2_list_buckets"} {
				if got := srv.Last(t, call).Header.Values("X-Bz-Test-Mode"); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s X-Bz-Test-Mode = %q, want %q", call, got, tc.want)
				}
			}
		})
	}
}

func TestClientUserAgentOptionsReachTheWire(t *testing.T) {
	srv, _ := clientOptionsWire(t, UserAgent("app/1"), UserAgent("lib/2"))
	want := "lib/2 app/1 " + base.DefaultUserAgent
	for _, call := range []string{"b2_authorize_account", "b2_list_buckets"} {
		b2wiretest.AssertHeader(t, srv.Last(t, call), "User-Agent", want)
	}
}

func TestKeyLifetimeAndDeadlineSendValidDuration(t *testing.T) {
	create := func(t *testing.T, opts ...KeyOption) b2wiretest.Request {
		t.Helper()
		srv, client := clientOptionsWire(t)
		if _, err := client.CreateKey(context.Background(), "k", append([]KeyOption{Capabilities("readFiles")}, opts...)...); err != nil {
			t.Fatal(err)
		}
		return srv.Last(t, "b2_create_key")
	}
	t.Run("no option omits the field", func(t *testing.T) {
		b2wiretest.AssertNoJSON(t, create(t), "validDurationInSeconds")
	})
	t.Run("lifetime", func(t *testing.T) {
		b2wiretest.AssertJSON(t, create(t, Lifetime(90*time.Second)), "validDurationInSeconds", 90)
	})
	t.Run("deadline is converted to the time left", func(t *testing.T) {
		r := create(t, Deadline(time.Now().Add(2*time.Hour)))
		got, ok := r.JSON["validDurationInSeconds"].(float64)
		if !ok {
			t.Fatalf("validDurationInSeconds = %v, want a number", r.JSON["validDurationInSeconds"])
		}
		// Deadline measures from now, so allow the seconds the test itself takes.
		if got > 7200 || got < 7200-30 {
			t.Errorf("validDurationInSeconds = %v, want about 7200", got)
		}
	})
}

// lockWatcher records whether it is held, so a test can see if a listing
// request ran under the lock.
type lockWatcher struct {
	mu      sync.Mutex
	held    bool
	locks   int
	unlocks int
}

func (l *lockWatcher) Lock() {
	l.mu.Lock()
	l.held = true
	l.locks++
	l.mu.Unlock()
}

func (l *lockWatcher) Unlock() {
	l.mu.Lock()
	l.held = false
	l.unlocks++
	l.mu.Unlock()
}

func (l *lockWatcher) isHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held
}

func TestListLockerIsHeldDuringEachListRequest(t *testing.T) {
	srv := b2wiretest.New(t)
	locker := &lockWatcher{}
	var heldDuringRequest []bool
	var mu sync.Mutex
	srv.Handle("b2_list_file_names", func(r b2wiretest.Request) b2wiretest.Response {
		mu.Lock()
		heldDuringRequest = append(heldDuringRequest, locker.isHeld())
		mu.Unlock()
		if r.JSON["startFileName"] == nil {
			return b2wiretest.Response{Body: `{"files":[{"fileId":"1","fileName":"a","action":"upload","uploadTimestamp":1}],"nextFileName":"b","nextFileId":null}`}
		}
		return b2wiretest.Response{Body: `{"files":[{"fileId":"2","fileName":"b","action":"upload","uploadTimestamp":1}],"nextFileName":null,"nextFileId":null}`}
	})
	bucket := newListingWireBucket(t, context.Background(), srv)

	it := bucket.List(context.Background(), ListLocker(locker), ListPageSize(1))
	var names []string
	for it.Next() {
		names = append(names, it.Object().Name())
	}
	if err := it.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed %q, want %q", names, want)
	}
	if want := []bool{true, true}; !reflect.DeepEqual(heldDuringRequest, want) {
		t.Errorf("lock held during list requests = %v, want %v", heldDuringRequest, want)
	}
	if locker.isHeld() || locker.locks != locker.unlocks {
		t.Errorf("after listing: held=%v locks=%d unlocks=%d, want released and balanced", locker.isHeld(), locker.locks, locker.unlocks)
	}
}

func TestStatusCountsRPCsByMethod(t *testing.T) {
	_, client := clientOptionsWire(t)
	status := client.Status()
	if len(status.RPCs) == 0 {
		t.Fatal("Status().RPCs has no time windows")
	}
	for window, methods := range status.RPCs {
		counts := methods.CountByMethod()
		if counts["b2_list_buckets"] != 1 {
			t.Errorf("window %v: b2_list_buckets count = %d, want 1 (all counts %v)", window, counts["b2_list_buckets"], counts)
		}
	}
}
