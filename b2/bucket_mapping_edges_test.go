package b2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestMappingComposeCopyMinimumPart(t *testing.T) {
	// https://www.backblaze.com/docs/cloud-storage-create-large-files-with-the-native-api
	for _, tc := range []struct {
		name, byteRange string
		valid           bool
	}{
		{"exact minimum", "bytes=0-4999999", true},
		{"one byte short", "bytes=0-4999998", false},
		{"one byte short at offset", "bytes=1-4999999", false},
		{"large offset", "bytes=9000000000-9004999999", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := PartFromCopy("source-id", tc.byteRange).validate(true)
			if tc.valid && err != nil {
				t.Fatalf("validate(%q) = %v, want nil", tc.byteRange, err)
			}
			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "at least 5000000")) {
				t.Fatalf("validate(%q) = %v, want minimum-part error", tc.byteRange, err)
			}
		})
	}
}

// Done is observed only by the SDK: the transport unwraps this context before HTTP.
type mappingRefreshWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *mappingRefreshWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestMappingReauthWaiterSurvivesCancelledLeader(t *testing.T) {
	// A live request must not inherit another request's cancellation (context.Context contract).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	leaderCtx, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()
	srv := b2wiretest.New(t)
	leaderAuthorizing := make(chan struct{})
	// The leader's reauthorization never gets an answer while the test runs, so
	// the leader can only end through its cancelled context. Answering it after
	// the cancel would race: a stale token could count as a successful refresh.
	// Registered after b2wiretest.New, so it runs before the server's cleanup,
	// which waits for this handler to return.
	releaseLeaderAuth := make(chan struct{})
	t.Cleanup(func() { close(releaseLeaderAuth) })
	var authCalls atomic.Int64
	srv.Handle("b2_authorize_account", func(b2wiretest.Request) b2wiretest.Response {
		call := authCalls.Add(1)
		if call == 2 {
			close(leaderAuthorizing)
			<-releaseLeaderAuth
		}
		body := fmt.Sprintf(updateAuthJSON, srv.URL())
		if call > 2 {
			body = strings.Replace(body, `"authorizationToken":"t"`, `"authorizationToken":"fresh"`, 1)
		}
		return b2wiretest.Response{Status: http.StatusOK, Body: body}
	})
	srv.Handle("b2_list_buckets", func(r b2wiretest.Request) b2wiretest.Response {
		if r.Header.Get("Authorization") == "t" {
			return b2wiretest.Response{Status: http.StatusUnauthorized, Body: `{"status":401,"code":"expired_auth_token","message":"expired"}`}
		}
		return b2wiretest.Response{Status: http.StatusOK, Body: `{"buckets":[]}`}
	})
	transport := stalledRefreshTransport(func(r *http.Request) (*http.Response, error) {
		if observed, ok := r.Context().(*mappingRefreshWaitContext); ok {
			r = r.WithContext(observed.Context)
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	leaderResult := make(chan error, 1)
	go func() {
		_, err := client.ListBuckets(leaderCtx)
		leaderResult <- err
	}()
	select {
	case <-leaderAuthorizing:
	case <-ctx.Done():
		t.Fatal("leader did not enter authorization:", ctx.Err())
	}
	waiterCtx := &mappingRefreshWaitContext{Context: ctx, waiting: make(chan struct{})}
	waiterResult := make(chan error, 1)
	go func() {
		_, err := client.ListBuckets(waiterCtx)
		waiterResult <- err
	}()
	select {
	case <-waiterCtx.waiting:
	case <-ctx.Done():
		t.Fatal("waiter did not enter the refresh wait:", ctx.Err())
	}
	cancelLeader()
	select {
	case err := <-leaderResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", err)
		}
	case <-ctx.Done():
		t.Fatal("leader did not finish:", ctx.Err())
	}
	select {
	case err := <-waiterResult:
		if err != nil {
			t.Fatalf("waiter error = %v, want nil", err)
		}
	case <-ctx.Done():
		t.Fatal("waiter did not finish:", ctx.Err())
	}
	if got := authCalls.Load(); got != 3 {
		t.Fatalf("authorization requests = %d, want 3 (initial, cancelled leader, live waiter)", got)
	}
	lists := srv.Calls("b2_list_buckets")
	if len(lists) != 3 {
		t.Fatalf("list requests = %d, want 3", len(lists))
	}
	for i, token := range []string{"t", "t", "fresh"} {
		b2wiretest.AssertHeader(t, lists[i], "Authorization", token)
	}
}

func newMappingBucket(t *testing.T, response string) (*Bucket, *b2wiretest.Server) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, response)
	srv.Reply("b2_update_bucket", http.StatusOK, response)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket, srv
}

func TestMappingRulesOnlyReplicationUpdate(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-update-bucket
	bucket, srv := newMappingBucket(t, updateBucketJSON)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rule := ReplicationRules{ReplicationRuleName: "to-destination", DestinationBucketID: "dest-id", FileNamePrefix: "logs/", IsEnabled: true, Priority: 1}
	err := bucket.Update(ctx, &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{AsReplicationSource: AsReplicationSource{ReplicationRules: []ReplicationRules{rule}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := srv.Last(t, "b2_update_bucket")
	b2wiretest.AssertJSON(t, r, "replicationConfiguration.asReplicationSource.replicationRules", []map[string]any{{
		"replicationRuleName": "to-destination", "destinationBucketId": "dest-id", "fileNamePrefix": "logs/",
		"includeExistingFiles": false, "isEnabled": true, "priority": 1,
	}})
}

func TestMappingLifecycleUpdateKeepsCORS(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-update-bucket
	const withCORS = `{"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
		`"corsRules":[{"corsRuleName":"browser","allowedOrigins":["https://example.com"],"allowedOperations":["b2_download_file_by_name"]}],` +
		`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},` +
		`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`
	bucket, srv := newMappingBucket(t, withCORS)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+withCORS+`]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := bucket.Update(ctx, &BucketAttrs{LifecycleRules: []LifecycleRule{{Prefix: "logs/", DaysNewUntilHidden: 1}}}); err != nil {
		t.Fatal(err)
	}
	r := srv.Last(t, "b2_update_bucket")
	if _, ok := r.JSON["corsRules"]; ok {
		b2wiretest.AssertJSON(t, r, "corsRules", []map[string]any{{"corsRuleName": "browser", "allowedOrigins": []string{"https://example.com"}, "allowedOperations": []string{"b2_download_file_by_name"}}})
	}
	attrs, err := bucket.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantCORS := []CORSRule{{Name: "browser", AllowedOrigins: []string{"https://example.com"}, AllowedOperations: []string{"b2_download_file_by_name"}}}
	if !reflect.DeepEqual(attrs.CORSRules, wantCORS) {
		t.Fatalf("Attrs().CORSRules = %+v, want %+v", attrs.CORSRules, wantCORS)
	}
}

func TestMappingReplicationPriorityBounds(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-update-bucket
	priorities := []struct {
		name     string
		priority int
		valid    bool
	}{{"maximum", 2147483647, true}, {"zero", 0, false}}
	if strconv.IntSize == 64 {
		tooHigh := int64(2147483648)
		priorities = append(priorities, struct {
			name     string
			priority int
			valid    bool
		}{"above maximum", int(tooHigh), false})
	}
	for _, tc := range priorities {
		t.Run(tc.name, func(t *testing.T) {
			root := &mappingPriorityRoot{replicationTestRoot: &replicationTestRoot{testRoot: &testRoot{errs: &errCont{}}}}
			src := &replicationTestBucket{testBucket: &testBucket{n: "source", idv: "src", errs: &errCont{}}}
			dst := &replicationTestBucket{testBucket: &testBucket{n: "destination", idv: "dst", errs: &errCont{}}}
			root.buckets = map[string]*replicationTestBucket{"source": src, "destination": dst}
			client := &Client{backend: &beRoot{b2i: root}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			from, err := client.Bucket(ctx, "source")
			if err != nil {
				t.Fatal(err)
			}
			to, err := client.Bucket(ctx, "destination")
			if err != nil {
				t.Fatal(err)
			}
			root.lists = 0
			err = SetupReplication(ctx, from, to, "source-key", "destination-key", WithRulePriority(tc.priority))
			if tc.valid {
				if err != nil {
					t.Fatalf("SetupReplication(%d) = %v, want nil", tc.priority, err)
				}
				if len(src.updates) != 1 || src.updates[0].ReplicationConfig.AsReplicationSource.ReplicationRules[0].Priority != 2147483647 {
					t.Fatalf("source updates = %+v, want priority 2147483647", src.updates)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "priority must be between 1 and 2147483647") {
				t.Fatalf("SetupReplication(%d) = %v, want priority bounds error", tc.priority, err)
			}
			if root.lists != 0 || len(src.updates) != 0 || len(dst.updates) != 0 || root.keyCalls != 0 {
				t.Fatalf("SetupReplication(%d) requests = %d lists, %d source updates, %d destination updates, %d keys; want 0", tc.priority, root.lists, len(src.updates), len(dst.updates), root.keyCalls)
			}
		})
	}
}

type mappingPriorityRoot struct {
	*replicationTestRoot
	lists int
}

func (r *mappingPriorityRoot) listBuckets(ctx context.Context, name string, types ...string) ([]b2BucketInterface, error) {
	r.lists++
	return r.replicationTestRoot.listBuckets(ctx, name, types...)
}
