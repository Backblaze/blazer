package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
)

const replBucketHead = `"bucketId":"bid","bucketName":"n","bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":2,` +
	`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}}`

const (
	noReplicationBucketJSON = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":null,"asReplicationDestination":null}}}`
	sourceReplBucketJSON    = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":{"sourceApplicationKeyId":"cached-key","replicationRules":[{"replicationRuleName":"cached-rule","destinationBucketId":"dest","fileNamePrefix":"","includeExistingFiles":false,"isEnabled":true,"priority":1}]},"asReplicationDestination":null}}}`
	bothReplBucketJSON      = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":{"asReplicationSource":{"sourceApplicationKeyId":"cached-key","replicationRules":[{"replicationRuleName":"cached-rule","destinationBucketId":"dest","fileNamePrefix":"","includeExistingFiles":false,"isEnabled":true,"priority":1}]},"asReplicationDestination":{"sourceToDestinationKeyMapping":{"source-key":"destination-key"}}}}}`
	filteredReplBucketJSON  = `{` + replBucketHead + `,"replicationConfiguration":{"isClientAuthorizedToRead":false,"value":{"asReplicationSource":null,"asReplicationDestination":{"sourceToDestinationKeyMapping":{"source-key":"destination-key"}}}}}`
)

// replFixture serves b2_authorize_account, b2_create_bucket and b2_update_bucket,
// records every b2_update_bucket request body, and answers each update with the
// next entry of updateResponses (the last one repeats).
type replFixture struct {
	bucket  *b2Bucket
	updates []map[string]any
	srv     *httptest.Server
}

func newReplFixture(t *testing.T, updateResponses ...string) *replFixture {
	t.Helper()
	f := &replFixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			fmt.Fprintf(w, updateAuthJSON, f.srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_create_bucket"):
			fmt.Fprint(w, noReplicationBucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			body, _ := io.ReadAll(r.Body)
			sent := map[string]any{}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			f.updates = append(f.updates, sent)
			resp := updateResponses[len(updateResponses)-1]
			if len(f.updates) <= len(updateResponses) {
				resp = updateResponses[len(f.updates)-1]
			}
			fmt.Fprint(w, resp)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)

	ctx := context.Background()
	root, err := base.AuthorizeAccount(ctx, "a", "k", base.SetAPIBase(f.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bb, err := root.CreateBucket(ctx, "n", "allPrivate", nil, nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	f.bucket = &b2Bucket{b: bb}
	return f
}

func (f *replFixture) lastReplication(t *testing.T) any {
	t.Helper()
	if len(f.updates) == 0 {
		t.Fatal("no b2_update_bucket request was sent")
	}
	return f.updates[len(f.updates)-1]["replicationConfiguration"]
}

// A bucket that already carries a replication config, updated by a caller who
// leaves ReplicationConfig unset, must not panic and must not change the config.
func TestReplConfig_NilDeref(t *testing.T) {
	f := newReplFixture(t, sourceReplBucketJSON)
	ctx := context.Background()

	// The first update's response seeds the handle's cached replication config.
	if err := f.bucket.updateBucket(ctx, &BucketAttrs{Info: map[string]string{"a": "b"}}); err != nil {
		t.Fatalf("seeding update: %v", err)
	}
	seeded := f.lastReplication(t)

	if err := f.bucket.updateBucket(ctx, &BucketAttrs{Info: map[string]string{"key": "value"}}); err != nil {
		t.Fatalf("updateBucket with ReplicationConfig omitted: %v", err)
	}
	got := f.lastReplication(t)
	want := map[string]any{"asReplicationSource": map[string]any{
		"sourceApplicationKeyId": "cached-key",
		"replicationRules": []any{map[string]any{
			"replicationRuleName": "cached-rule", "destinationBucketId": "dest",
			"fileNamePrefix": "", "includeExistingFiles": false, "isEnabled": true, "priority": float64(1),
		}},
	}}
	if seeded != nil {
		t.Errorf("first update sent replicationConfiguration %v, want none (cache was empty)", seeded)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replicationConfiguration after omitting the caller config = %v, want the cached config %v", got, want)
	}
}

// A caller-supplied ReplicationConfig must reach the request even when the bucket
// handle has no cached replication config.
func TestReplConfig_SilentDrop(t *testing.T) {
	f := newReplFixture(t, noReplicationBucketJSON)

	err := f.bucket.updateBucket(context.Background(), &BucketAttrs{
		ReplicationConfig: &ReplicationConfiguration{
			AsReplicationSource: AsReplicationSource{
				SourceApplicationKeyID: "source-key",
				ReplicationRules: []ReplicationRules{{
					ReplicationRuleName: "rule",
					DestinationBucketID: "dest-id",
					IsEnabled:           true,
					Priority:            1,
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("updateBucket: %v", err)
	}

	cfg, ok := f.lastReplication(t).(map[string]any)
	if !ok {
		t.Fatalf("request replicationConfiguration = %v, want the caller's config", f.lastReplication(t))
	}
	src, _ := cfg["asReplicationSource"].(map[string]any)
	if src["sourceApplicationKeyId"] != "source-key" {
		t.Errorf("sourceApplicationKeyId = %v, want source-key", src["sourceApplicationKeyId"])
	}
	rules, _ := src["replicationRules"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["destinationBucketId"] != "dest-id" {
		t.Errorf("replicationRules = %v, want one rule to dest-id", src["replicationRules"])
	}
}

func TestReplConfig_SourceUpdatePreservesDestination(t *testing.T) {
	f := newReplFixture(t, bothReplBucketJSON)
	ctx := context.Background()
	if err := f.bucket.updateBucket(ctx, &BucketAttrs{Info: map[string]string{"seed": "yes"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.bucket.updateBucket(ctx, &BucketAttrs{ReplicationConfig: &ReplicationConfiguration{
		AsReplicationSource: AsReplicationSource{SourceApplicationKeyID: "new-source-key"},
	}}); err != nil {
		t.Fatal(err)
	}
	cfg, ok := f.lastReplication(t).(map[string]any)
	if !ok {
		t.Fatalf("replicationConfiguration = %v, want both roles", f.lastReplication(t))
	}
	dest, ok := cfg["asReplicationDestination"].(map[string]any)
	if !ok || !reflect.DeepEqual(dest["sourceToDestinationKeyMapping"], map[string]any{"source-key": "destination-key"}) {
		t.Errorf("asReplicationDestination = %v, want cached mapping", cfg["asReplicationDestination"])
	}
	source, ok := cfg["asReplicationSource"].(map[string]any)
	if !ok || source["sourceApplicationKeyId"] != "new-source-key" {
		t.Errorf("asReplicationSource = %v, want new source key", cfg["asReplicationSource"])
	}
}

func newListReplFixture(t *testing.T, bucketJSON string, canRead bool) (*Client, *[]map[string]any) {
	t.Helper()
	updates := &[]map[string]any{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			auth := fmt.Sprintf(updateAuthJSON, srv.URL)
			if canRead {
				auth = strings.ReplaceAll(auth, `"capabilities":[]`, `"capabilities":["readBucketReplications"]`)
			}
			fmt.Fprint(w, auth)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			fmt.Fprintf(w, `{"buckets":[%s]}`, bucketJSON)
		case strings.HasSuffix(r.URL.Path, "/b2_update_bucket"):
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read update body: %v", err)
			}
			sent := map[string]any{}
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			*updates = append(*updates, sent)
			fmt.Fprint(w, bucketJSON)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := NewClient(context.Background(), "a", "k", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return client, updates
}

func TestReplConfig_ReadModifyWriteBothRoles(t *testing.T) {
	client, updates := newListReplFixture(t, bothReplBucketJSON, true)
	ctx := context.Background()
	buckets, err := client.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 {
		t.Fatalf("ListBuckets = %v, %v; want one bucket", buckets, err)
	}
	listed := buckets[0].b.attrs().ReplicationConfig
	if listed == nil || listed.AsReplicationDestination == nil || listed.AsReplicationSource.SourceApplicationKeyID != "cached-key" {
		t.Fatalf("ListBuckets replication config = %+v, want both roles", listed)
	}
	attrs, err := buckets[0].Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attrs.ReplicationConfig, listed) {
		t.Fatalf("Attrs replication config = %+v, want %+v", attrs.ReplicationConfig, listed)
	}
	if err := buckets[0].Update(ctx, attrs); err != nil {
		t.Fatal(err)
	}
	cfg, ok := (*updates)[0]["replicationConfiguration"].(map[string]any)
	if !ok {
		t.Fatalf("update replicationConfiguration = %v, want both roles", (*updates)[0]["replicationConfiguration"])
	}
	var original map[string]any
	if err := json.Unmarshal([]byte(bothReplBucketJSON), &original); err != nil {
		t.Fatal(err)
	}
	want := original["replicationConfiguration"].(map[string]any)["value"]
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("round-trip replication config = %v, want %v", cfg, want)
	}
}

func TestReplConfig_FilteredReadOmitsWrite(t *testing.T) {
	client, updates := newListReplFixture(t, filteredReplBucketJSON, false)
	ctx := context.Background()
	buckets, err := client.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 {
		t.Fatalf("ListBuckets = %v, %v; want one bucket", buckets, err)
	}
	attrs, err := buckets[0].Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.ReplicationConfig != nil {
		t.Fatalf("filtered Attrs replication config = %+v, want unknown/nil", attrs.ReplicationConfig)
	}
	if err := buckets[0].Update(ctx, &BucketAttrs{Info: map[string]string{"other": "change"}}); err != nil {
		t.Fatal(err)
	}
	if _, sent := (*updates)[0]["replicationConfiguration"]; sent {
		t.Errorf("update sent replicationConfiguration with filtered key: %v", (*updates)[0]["replicationConfiguration"])
	}
}
