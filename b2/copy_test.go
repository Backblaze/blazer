package b2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// copyFixture serves two buckets, "a" and "b", and records every b2_copy_file
// request body.
type copyFixture struct {
	t          *testing.T
	copies     []map[string]any
	client     *Client
	srv        *httptest.Server
	listAction string // action reported for the copied version in listings and get_file_info
}

func newCopyFixture(t *testing.T) *copyFixture {
	t.Helper()
	f := &copyFixture{t: t}
	ids := map[string]string{"a": "bucket-a-id", "b": "bucket-b-id"}
	bucketJSON := func(name string) string {
		return fmt.Sprintf(`{"bucketId":%q,"bucketName":%q,"bucketType":"allPrivate","bucketInfo":{},"lifecycleRules":[],"revision":1,`+
			`"fileLockConfiguration":{"isClientAuthorizedToRead":true,"value":{"isFileLockEnabled":false,"defaultRetention":{"mode":null,"period":null}}},`+
			`"replicationConfiguration":{"isClientAuthorizedToRead":true,"value":null}}`, ids[name], name)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/b2_authorize_account"):
			_, _ = fmt.Fprintf(w, updateAuthJSON, f.srv.URL)
		case strings.HasSuffix(r.URL.Path, "/b2_list_buckets"):
			var req struct {
				Name string `json:"bucketName"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			_, _ = fmt.Fprintf(w, `{"buckets":[%s]}`, bucketJSON(req.Name))
		case strings.HasSuffix(r.URL.Path, "/b2_list_file_versions"):
			_, _ = fmt.Fprintf(w, `{"files":[{"fileId":"copied-id","fileName":"dst","accountId":"a","bucketId":"x","contentLength":5,"contentSha1":"x","contentType":"text/plain","fileInfo":{},"action":%q,"uploadTimestamp":1},`+
				`{"fileId":"big-id","fileName":"unfinished","accountId":"a","bucketId":"x","contentLength":0,"contentSha1":"none","contentType":"text/plain","fileInfo":{},"action":"start","uploadTimestamp":1}],"nextFileName":null,"nextFileId":null}`, f.listAction)
		case strings.HasSuffix(r.URL.Path, "/b2_get_file_info"):
			_, _ = fmt.Fprintf(w, `{"fileId":"copied-id","fileName":"dst","accountId":"a","bucketId":"x","contentLength":5,"contentSha1":"x","contentType":"text/plain","fileInfo":{},"action":%q,"uploadTimestamp":1}`, f.listAction)
		case strings.HasSuffix(r.URL.Path, "/b2_copy_file"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.copies = append(f.copies, body)
			_, _ = fmt.Fprintf(w, `{"fileId":"copied-id","fileName":%q,"accountId":"a","bucketId":"x","contentLength":5,"contentSha1":"x","contentType":"text/plain","fileInfo":{},"action":"copy","uploadTimestamp":1}`, body["fileName"])
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	c, err := NewClient(context.Background(), "account", "key", APIBase(f.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	f.client = c
	return f
}

func (f *copyFixture) bucket(name string) *Bucket {
	f.t.Helper()
	b, err := f.client.Bucket(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

// Per the b2_copy_file docs, omitting destinationBucketId copies into the
// source file's bucket, not the caller's. Copy on bucket b must write to b.
func TestCopyDefaultsToTheReceiverBucket(t *testing.T) {
	f := newCopyFixture(t)
	a, b := f.bucket("a"), f.bucket("b")
	_ = a
	obj, err := b.Copy(context.Background(), "source-file-in-bucket-a", "dst")
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := f.copies[0]["destinationBucketId"]; got != "bucket-b-id" {
		t.Errorf("destinationBucketId = %v, want the receiver bucket bucket-b-id", got)
	}
	if obj.b != b || obj.Name() != "dst" {
		t.Errorf("returned object = bucket %v name %q, want the receiver bucket and name dst", obj.b.Name(), obj.Name())
	}
}

func TestCopyToBucketSendsThatBucketID(t *testing.T) {
	f := newCopyFixture(t)
	a, b := f.bucket("a"), f.bucket("b")
	obj, err := a.Copy(context.Background(), "src", "dst", CopyToBucket(b))
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := f.copies[0]["destinationBucketId"]; got != "bucket-b-id" {
		t.Errorf("destinationBucketId = %v, want bucket-b-id", got)
	}
	if obj.b != b {
		t.Errorf("returned object belongs to %q, want the destination bucket b", obj.b.Name())
	}
}

func TestCopyMetadataDirectiveAndRangeOnTheWire(t *testing.T) {
	f := newCopyFixture(t)
	b := f.bucket("b")
	ctx := context.Background()

	if _, err := b.Copy(ctx, "src", "plain"); err != nil {
		t.Fatal(err)
	}
	if got := f.copies[0]["metadataDirective"]; got != "COPY" {
		t.Errorf("default metadataDirective = %v, want COPY", got)
	}
	for _, k := range []string{"contentType", "fileInfo", "range"} {
		if v, ok := f.copies[0][k]; ok {
			t.Errorf("default copy sent %s = %v", k, v)
		}
	}

	info := map[string]string{"k": "v"}
	if _, err := b.Copy(ctx, "src", "replaced", CopyWithMetadata(ReplaceMetadata, "text/plain", info), CopyRange("bytes=0-99")); err != nil {
		t.Fatal(err)
	}
	got := f.copies[1]
	if got["metadataDirective"] != "REPLACE" || got["contentType"] != "text/plain" || got["range"] != "bytes=0-99" {
		t.Errorf("REPLACE request = %v", got)
	}
	if fi, _ := got["fileInfo"].(map[string]any); fi["k"] != "v" {
		t.Errorf("fileInfo = %v, want k=v", got["fileInfo"])
	}
}

// The docs call these 400 errors; fail before sending anything.
func TestCopyRejectsInvalidMetadataOptions(t *testing.T) {
	tests := []struct {
		name string
		opt  CopyOption
	}{
		{"COPY with a content type", CopyWithMetadata(CopyMetadata, "text/plain", nil)},
		{"COPY with file info", CopyWithMetadata(CopyMetadata, "", map[string]string{"k": "v"})},
		{"REPLACE without a content type", CopyWithMetadata(ReplaceMetadata, "", nil)},
		{"an unknown directive", CopyWithMetadata(MetadataDirective("MOVE"), "", nil)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newCopyFixture(t)
			b := f.bucket("b")
			if _, err := b.Copy(context.Background(), "src", "dst", tc.opt); err == nil {
				t.Error("Copy succeeded, want an error")
			}
			if len(f.copies) != 0 {
				t.Errorf("%d b2_copy_file request(s) were sent, want none", len(f.copies))
			}
		})
	}
}

// B2 reports a server-side copy with the action "copy". It is a regular
// completed object everywhere an "upload" is.
func TestCopiedVersionIsAListedAndCompletedObject(t *testing.T) {
	f := newCopyFixture(t)
	f.listAction = "copy"
	ctx := context.Background()
	b := f.bucket("b")

	var names []string
	iter := b.List(ctx, ListHidden())
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "dst" {
		t.Errorf("ListHidden = %v, want the copied version [dst] and not the unfinished upload", names)
	}

	obj, err := b.Copy(ctx, "src", "dst")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Status != Uploaded {
		t.Errorf("Attrs().Status for a copy = %v, want Uploaded", attrs.Status)
	}
}

func TestCopyWithReplaceAndNoInfoSendsAnEmptyFileInfo(t *testing.T) {
	f := newCopyFixture(t)
	b := f.bucket("b")
	if _, err := b.Copy(context.Background(), "src", "dst", CopyWithMetadata(ReplaceMetadata, "text/plain", nil)); err != nil {
		t.Fatal(err)
	}
	fi, ok := f.copies[0]["fileInfo"].(map[string]any)
	if !ok || len(fi) != 0 {
		t.Errorf("fileInfo = %v, want an explicit empty object", f.copies[0]["fileInfo"])
	}
}
