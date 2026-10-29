package b2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

// Wire fields below follow https://www.backblaze.com/apidocs/b2-hide-file
// and https://www.backblaze.com/apidocs/b2-delete-file-version.
func TestObjectHideWire(t *testing.T) {
	t.Run("sends name and bucket", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		srv.Handle("gone", liveOnlyHead("file-id", "gone"))
		srv.Reply("b2_hide_file", http.StatusOK, `{"fileId":"hide-id","action":"hide","uploadTimestamp":0}`)
		if err := bucket.Object("gone").Hide(context.Background()); err != nil {
			t.Fatal(err)
		}
		calls := srv.Calls("b2_hide_file")
		if len(calls) != 1 {
			t.Fatalf("b2_hide_file calls = %d, want 1", len(calls))
		}
		b2wiretest.AssertJSON(t, calls[0], "bucketId", "bid")
		b2wiretest.AssertJSON(t, calls[0], "fileName", "gone")
	})
	t.Run("returns service errors", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		srv.Handle("gone", liveOnlyHead("file-id", "gone"))
		srv.Reply("b2_hide_file", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"cannot hide"}`)
		if err := bucket.Object("gone").Hide(context.Background()); err == nil {
			t.Fatal("Hide succeeded after a scripted 400")
		}
	})
}

func TestBucketRevealWire(t *testing.T) {
	t.Run("deletes newest hide marker", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		// b2_list_file_versions returns versions in newest-first order.
		srv.Reply("b2_list_file_versions", http.StatusOK, `{"files":[{"fileId":"hide-id","fileName":"gone","action":"hide","uploadTimestamp":2},{"fileId":"upload-id","fileName":"gone","action":"upload","uploadTimestamp":1}],"nextFileName":"","nextFileId":""}`)
		srv.Reply("b2_delete_file_version", http.StatusOK, `{}`)
		if err := bucket.Reveal(context.Background(), "gone"); err != nil {
			t.Fatal(err)
		}
		request := srv.Last(t, "b2_delete_file_version")
		b2wiretest.AssertJSON(t, request, "fileId", "hide-id")
		b2wiretest.AssertJSON(t, request, "fileName", "gone")
	})
	t.Run("leaves a current upload alone", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		// b2_list_file_versions returns versions in newest-first order.
		srv.Reply("b2_list_file_versions", http.StatusOK, `{"files":[{"fileId":"upload-id","fileName":"gone","action":"upload","uploadTimestamp":2},{"fileId":"hide-id","fileName":"gone","action":"hide","uploadTimestamp":1}],"nextFileName":"","nextFileId":""}`)
		if err := bucket.Reveal(context.Background(), "gone"); err != nil {
			t.Fatal(err)
		}
		if calls := srv.Calls("b2_delete_file_version"); len(calls) != 0 {
			t.Fatalf("b2_delete_file_version calls = %d, want 0", len(calls))
		}
	})
}

// Request fields follow https://www.backblaze.com/apidocs/b2-get-download-authorization.
// URL arguments follow https://www.backblaze.com/docs/cloud-storage-download-authorization.
func TestBucketAuthTokenWire(t *testing.T) {
	srv, bucket := liveOnlyBucket(t)
	srv.Reply("b2_get_download_authorization", http.StatusOK, `{"authorizationToken":"download-token"}`)
	token, err := bucket.AuthToken(context.Background(), "p/", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token != "download-token" {
		t.Errorf("token = %q, want %q", token, "download-token")
	}
	request := srv.Last(t, "b2_get_download_authorization")
	b2wiretest.AssertJSON(t, request, "bucketId", "bid")
	b2wiretest.AssertJSON(t, request, "fileNamePrefix", "p/")
	b2wiretest.AssertJSON(t, request, "validDurationInSeconds", 3600)
	b2wiretest.AssertNoJSON(t, request, "b2ContentDisposition")
}

func TestObjectAuthURLWire(t *testing.T) {
	srv, bucket := liveOnlyBucket(t)
	srv.Reply("b2_get_download_authorization", http.StatusOK, `{"authorizationToken":"download-token"}`)
	const (
		objectName  = "folder/a b.txt"
		disposition = `attachment; filename="a b.txt"`
	)
	object := bucket.Object(objectName)
	u, err := object.AuthURL(context.Background(), time.Hour, disposition)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("Authorization"); got != "download-token" {
		t.Errorf("Authorization = %q, want %q", got, "download-token")
	}
	if got := u.Query().Get("b2ContentDisposition"); got != disposition {
		t.Errorf("b2ContentDisposition = %q, want %q", got, disposition)
	}
	request := srv.Last(t, "b2_get_download_authorization")
	b2wiretest.AssertJSON(t, request, "fileNamePrefix", objectName)
	b2wiretest.AssertJSON(t, request, "b2ContentDisposition", disposition)
	u, err = object.AuthURL(context.Background(), time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := u.Query()["b2ContentDisposition"]; ok {
		t.Errorf("empty disposition URL query = %q, want b2ContentDisposition omitted", u.RawQuery)
	}
	request = srv.Last(t, "b2_get_download_authorization")
	b2wiretest.AssertNoJSON(t, request, "b2ContentDisposition")
}

// Request and response fields follow https://www.backblaze.com/apidocs/b2-list-keys
// and https://www.backblaze.com/apidocs/b2-delete-key.
func TestClientListKeysAndDeleteWire(t *testing.T) {
	srv := b2wiretest.New(t)
	client := liveOnlyClient(t, srv)
	srv.ReplySequence("b2_list_keys",
		b2wiretest.Response{Status: http.StatusOK, Body: `{"keys":[{"applicationKeyId":"k-1","keyName":"first","capabilities":["listBuckets"],"expirationTimestamp":1700000000123},{"applicationKeyId":"k-2","keyName":"second","capabilities":["readFiles"],"expirationTimestamp":0}],"nextApplicationKeyId":"k-3"}`},
		b2wiretest.Response{Status: http.StatusOK, Body: `{"keys":[]}`},
	)
	srv.Reply("b2_delete_key", http.StatusOK, `{}`)
	keys, next, err := client.ListKeys(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || next != "k-3" {
		t.Fatalf("ListKeys = %d keys, %q, want 2 keys, k-3", len(keys), next)
	}
	// Capabilities mapping is tracked separately in #220.
	if keys[0].ID() != "k-1" || keys[0].Name() != "first" || keys[0].Expires().UnixMilli() != 1700000000123 {
		t.Errorf("first key = id=%q name=%q expires=%d", keys[0].ID(), keys[0].Name(), keys[0].Expires().UnixMilli())
	}
	if keys[1].ID() != "k-2" || keys[1].Name() != "second" || keys[1].Expires().UnixMilli() != 0 {
		t.Errorf("second key = id=%q name=%q expires=%v", keys[1].ID(), keys[1].Name(), keys[1].Expires())
	}
	request := srv.Last(t, "b2_list_keys")
	b2wiretest.AssertJSON(t, request, "accountId", "a")
	b2wiretest.AssertJSON(t, request, "maxKeyCount", 10)
	b2wiretest.AssertNoJSON(t, request, "startApplicationKeyId")
	if _, _, err := client.ListKeys(context.Background(), 10, next); !errors.Is(err, io.EOF) {
		t.Errorf("second ListKeys error = %v, want io.EOF", err)
	}
	request = srv.Last(t, "b2_list_keys")
	b2wiretest.AssertJSON(t, request, "startApplicationKeyId", "k-3")
	if err := keys[0].Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	request = srv.Last(t, "b2_delete_key")
	b2wiretest.AssertJSON(t, request, "applicationKeyId", "k-1")
}

// Request fields follow https://www.backblaze.com/apidocs/b2-cancel-large-file.
func TestObjectCancelWire(t *testing.T) {
	t.Run("unfinished file", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"large-id","fileName":"pending","action":"start","uploadTimestamp":0}],"nextFileId":""}`)
		srv.Reply("b2_cancel_large_file", http.StatusOK, `{}`)
		iter := bucket.List(context.Background(), ListUnfinished())
		if !iter.Next() {
			t.Fatalf("unfinished list returned no object: %v", iter.Err())
		}
		if err := iter.Object().Cancel(context.Background()); err != nil {
			t.Fatal(err)
		}
		request := srv.Last(t, "b2_cancel_large_file")
		b2wiretest.AssertJSON(t, request, "fileId", "large-id")
	})
	t.Run("uploaded file", func(t *testing.T) {
		srv, bucket := liveOnlyBucket(t)
		srv.Reply("b2_list_file_names", http.StatusOK, `{"files":[{"fileId":"upload-id","fileName":"complete","action":"upload","uploadTimestamp":0}],"nextFileName":""}`)
		iter := bucket.List(context.Background())
		if !iter.Next() {
			t.Fatalf("current list returned no object: %v", iter.Err())
		}
		if err := iter.Object().Cancel(context.Background()); err == nil {
			t.Fatal("Cancel succeeded for an uploaded object")
		}
		if calls := srv.Calls("b2_cancel_large_file"); len(calls) != 0 {
			t.Fatalf("b2_cancel_large_file calls = %d, want 0", len(calls))
		}
	})
}

func liveOnlyClient(t *testing.T, srv *b2wiretest.Server) *Client {
	t.Helper()
	client, err := NewClient(context.Background(), "a", "k", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func liveOnlyBucket(t *testing.T) (*b2wiretest.Server, *Bucket) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, updateBucketJSON)
	bucket, err := liveOnlyClient(t, srv).NewBucket(context.Background(), "n", nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv, bucket
}

func liveOnlyHead(id, name string) func(b2wiretest.Request) b2wiretest.Response {
	return func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.Response{Status: http.StatusOK, Header: http.Header{
			"Content-Length": {"0"},
			"X-Bz-File-Id":   {id},
			"X-Bz-File-Name": {name},
		}}
	}
}
