package b2

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestComposeUploadsAndCopiesInOrder(t *testing.T) {
	const size = 5e6
	var calls []string
	var copyBody, finishBody map[string]any
	var partNumber string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b2api/v4/b2_authorize_account":
			_, _ = fmt.Fprintf(w, updateAuthJSON, srv.URL)
		case "/b2api/v4/b2_list_buckets":
			_, _ = fmt.Fprint(w, `{"buckets":[]}`)
		case "/b2api/v4/b2_create_bucket":
			_, _ = fmt.Fprint(w, noReplicationBucketJSON)
		case "/b2api/v4/b2_start_large_file":
			calls = append(calls, "start")
			_, _ = fmt.Fprint(w, `{"fileId":"large"}`)
		case "/b2api/v4/b2_get_upload_part_url":
			_, _ = fmt.Fprintf(w, `{"uploadUrl":"%s/part","authorizationToken":"part-token"}`, srv.URL)
		case "/part":
			calls = append(calls, "upload")
			partNumber = r.Header.Get("X-Bz-Part-Number")
			body, _ := io.ReadAll(r.Body)
			if len(body) != size || r.Header.Get("Authorization") != "part-token" {
				t.Errorf("upload len=%d auth=%q", len(body), r.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprint(w, `{}`)
		case "/b2api/v4/b2_copy_part":
			calls = append(calls, "copy")
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &copyBody); err != nil {
				t.Error(err)
			}
			if r.Header.Get("Authorization") != "t" {
				t.Errorf("copy auth = %q", r.Header.Get("Authorization"))
			}
			_, _ = fmt.Fprint(w, `{"fileId":"large","partNumber":2,"contentLength":3,"contentSha1":"copy-sha"}`)
		case "/b2api/v4/b2_finish_large_file":
			calls = append(calls, "finish")
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &finishBody); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprint(w, `{"fileId":"large","fileName":"composed","action":"upload"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	client, err := NewClient(ctx, "a", "k", APIBase(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "n", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Compose(ctx, "invalid", []ComposePart{PartFromReader(bytes.NewReader([]byte("x")), 1), PartFromCopy("src", "bytes=0-2")}); err == nil {
		t.Fatal("short nonfinal part accepted")
	}
	if len(calls) != 0 {
		t.Fatalf("invalid compose made calls: %v", calls)
	}
	payload := bytes.Repeat([]byte("x"), size)
	object, err := bucket.Compose(ctx, "composed", []ComposePart{PartFromReader(bytes.NewReader(payload), size), PartFromCopy("source-id", "bytes=0-2")})
	if err != nil {
		t.Fatal(err)
	}
	if object.ID() != "large" || partNumber != "1" {
		t.Errorf("object ID=%q upload part=%q", object.ID(), partNumber)
	}
	if want := []string{"start", "upload", "copy", "finish"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls=%v, want %v", calls, want)
	}
	if copyBody["sourceFileId"] != "source-id" || copyBody["largeFileId"] != "large" || copyBody["partNumber"] != float64(2) || copyBody["range"] != "bytes=0-2" {
		t.Errorf("copy body=%v", copyBody)
	}
	hash := fmt.Sprintf("%x", sha1.Sum(payload))
	if want := []any{hash, "copy-sha"}; !reflect.DeepEqual(finishBody["partSha1Array"], want) {
		t.Errorf("finish body=%v, want hashes %v", finishBody, want)
	}
}
