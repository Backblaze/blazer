package b2

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func listingPages(pages [2]string, cursor map[string]string) func(b2wiretest.Request) b2wiretest.Response {
	var requests atomic.Int32
	return func(r b2wiretest.Request) b2wiretest.Response {
		page := requests.Add(1)
		valid := page == 1 || page == 2
		for field, want := range cursor {
			got, present := r.JSON[field]
			if (page == 1 && present) || (page == 2 && got != want) {
				valid = false
			}
		}
		// Missing cursors after page 1 must fail instead of repeating page 1 forever.
		if !valid {
			return b2wiretest.Response{Status: http.StatusBadRequest,
				Body: `{"status":400,"code":"bad_request","message":"unexpected listing cursor"}`}
		}
		return b2wiretest.Response{Status: http.StatusOK, Body: pages[page-1]}
	}
}

func newListingWireBucket(t *testing.T, ctx context.Context, srv *b2wiretest.Server) *Bucket {
	t.Helper()
	bucketJSON := strings.Replace(updateBucketJSON, `"bucketName":"n"`, `"bucketName":"bucket"`, 1)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[`+bucketJSON+`]}`)
	client, err := NewClient(ctx, "a", "k", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(ctx, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func TestListSendsPrefixAndCursorOnEveryPage(t *testing.T) {
	for _, test := range []struct {
		name   string
		call   string
		opts   []ListOption
		pages  [2]string
		cursor map[string]string
	}{
		{
			// https://www.backblaze.com/apidocs/b2-list-file-names
			name: "current", call: "b2_list_file_names",
			opts: []ListOption{ListPrefix("logs/"), ListDelimiter("/")},
			pages: [2]string{
				`{"files":[{"fileName":"logs/a.txt","fileId":"id-a","action":"upload"},{"fileName":"logs/b.txt","fileId":"id-b","action":"upload"}],"nextFileName":"logs/c.txt"}`,
				`{"files":[{"fileName":"logs/c.txt","fileId":"id-c","action":"upload"}],"nextFileName":null}`,
			},
			cursor: map[string]string{"startFileName": "logs/c.txt"},
		},
		{
			// https://www.backblaze.com/apidocs/b2-list-file-versions
			name: "hidden", call: "b2_list_file_versions",
			opts: []ListOption{ListHidden(), ListPrefix("logs/"), ListDelimiter("/")},
			pages: [2]string{
				`{"files":[{"fileName":"logs/a.txt","fileId":"id-a","action":"upload"},{"fileName":"logs/b.txt","fileId":"id-b","action":"hide"}],"nextFileName":"logs/c.txt","nextFileId":"id-c"}`,
				`{"files":[{"fileName":"logs/c.txt","fileId":"id-c","action":"upload"}],"nextFileName":null,"nextFileId":null}`,
			},
			cursor: map[string]string{"startFileName": "logs/c.txt", "startFileId": "id-c"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Handle(test.call, listingPages(test.pages, test.cursor))
			bucket := newListingWireBucket(t, ctx, srv)
			iter := bucket.List(ctx, append(test.opts, ListPageSize(2))...)
			var names []string
			for iter.Next() {
				names = append(names, iter.Object().Name())
			}
			if err := iter.Err(); err != nil {
				t.Errorf("listing: %v", err)
			}
			if want := []string{"logs/a.txt", "logs/b.txt", "logs/c.txt"}; !reflect.DeepEqual(names, want) {
				t.Errorf("names = %v, want %v", names, want)
			}
			calls := srv.Calls(test.call)
			if len(calls) != 2 {
				t.Fatalf("%s requests = %d, want 2", test.call, len(calls))
			}
			for _, r := range calls {
				b2wiretest.AssertJSON(t, r, "bucketId", "bid")
				b2wiretest.AssertJSON(t, r, "maxFileCount", 2)
				b2wiretest.AssertJSON(t, r, "prefix", "logs/")
				b2wiretest.AssertJSON(t, r, "delimiter", "/")
			}
			for field, want := range test.cursor {
				b2wiretest.AssertNoJSON(t, calls[0], field)
				b2wiretest.AssertJSON(t, calls[1], field, want)
			}
		})
	}
}

func TestListUnfinishedSendsCursorOnEveryPage(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-unfinished-large-files
	// Add the prefix assertion when #184 makes ListUnfinished send it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := b2wiretest.New(t)
	const call = "b2_list_unfinished_large_files"
	pages := [2]string{
		`{"files":[{"fileName":"logs/a.txt","fileId":"id-a","action":"start"},{"fileName":"logs/b.txt","fileId":"id-b","action":"start"}],"nextFileId":"id-c"}`,
		`{"files":[{"fileName":"logs/c.txt","fileId":"id-c","action":"start"}],"nextFileId":null}`,
	}
	srv.Handle(call, listingPages(pages, map[string]string{"startFileId": "id-c"}))
	bucket := newListingWireBucket(t, ctx, srv)
	iter := bucket.List(ctx, ListUnfinished(), ListPageSize(2))
	var names []string
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Errorf("listing: %v", err)
	}
	if want := []string{"logs/a.txt", "logs/b.txt", "logs/c.txt"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	calls := srv.Calls(call)
	if len(calls) != 2 {
		t.Fatalf("%s requests = %d, want 2", call, len(calls))
	}
	for _, r := range calls {
		b2wiretest.AssertJSON(t, r, "bucketId", "bid")
		b2wiretest.AssertJSON(t, r, "maxFileCount", 2)
	}
	b2wiretest.AssertNoJSON(t, calls[0], "startFileId")
	b2wiretest.AssertJSON(t, calls[1], "startFileId", "id-c")
}

func TestListPrefixFromKeyOrCaller(t *testing.T) {
	for _, test := range []struct {
		name       string
		call       string
		restricted bool
		hidden     bool
		prefix     string
	}{
		// Listing prefix contracts:
		// https://www.backblaze.com/apidocs/b2-list-file-names
		// https://www.backblaze.com/apidocs/b2-list-file-versions
		{"restricted/names", "b2_list_file_names", true, false, "restricted/"},
		{"restricted/versions", "b2_list_file_versions", true, true, "restricted/"},
		{"caller/names", "b2_list_file_names", false, false, "logs/"},
		{"caller/versions", "b2_list_file_versions", false, true, "logs/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			var opts []ListOption
			if test.restricted {
				// v4 allowed.buckets/namePrefix shape from v4AuthJSON:
				// https://www.backblaze.com/apidocs/b2-authorize-account
				auth := strings.Replace(updateAuthJSON, `"buckets":null`, `"buckets":[{"id":"bid","name":"bucket"}]`, 1)
				auth = strings.Replace(auth, `"namePrefix":null`, `"namePrefix":"restricted/"`, 1)
				srv.Reply("b2_authorize_account", http.StatusOK, fmt.Sprintf(auth, srv.URL()))
			} else {
				opts = append(opts, ListPrefix("logs/"))
			}
			if test.hidden {
				opts = append(opts, ListHidden())
			}
			srv.Reply(test.call, http.StatusOK, `{"files":[],"nextFileName":null,"nextFileId":null}`)
			bucket := newListingWireBucket(t, ctx, srv)
			iter := bucket.List(ctx, opts...)
			if iter.Next() {
				t.Fatal("empty listing yielded an object")
			}
			if err := iter.Err(); err != nil {
				t.Fatal(err)
			}
			calls := srv.Calls(test.call)
			if len(calls) != 1 {
				t.Fatalf("%s requests = %d, want 1", test.call, len(calls))
			}
			b2wiretest.AssertJSON(t, calls[0], "prefix", test.prefix)
		})
	}
}
