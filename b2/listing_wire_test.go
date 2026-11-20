package b2

import (
	"context"
	"encoding/json"
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

func TestListDefaultPageSizeUsesB2Default(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-file-names documents the default as 100.
	for _, tc := range []struct {
		name string
		opts []ListOption
	}{
		{"no option", nil},
		{"explicit zero", []ListOption{ListPageSize(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			page := 0
			srv.Handle("b2_list_file_names", func(b2wiretest.Request) b2wiretest.Response {
				page++
				switch page {
				case 1:
					files := make([]map[string]string, 100)
					for i := range files {
						files[i] = map[string]string{"fileName": fmt.Sprintf("file-%03d", i+1), "fileId": fmt.Sprintf("id-%03d", i+1), "action": "upload"}
					}
					body, err := json.Marshal(map[string]any{"files": files, "nextFileName": "file-101"})
					if err != nil {
						t.Fatal(err)
					}
					return b2wiretest.Response{Status: http.StatusOK, Body: string(body)}
				case 2:
					return b2wiretest.Response{Status: http.StatusOK, Body: `{"files":[{"fileName":"file-101","fileId":"id-101","action":"upload"}],"nextFileName":null}`}
				default:
					return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"unexpected listing page"}`}
				}
			})
			bucket := newListingWireBucket(t, ctx, srv)
			iter := bucket.List(ctx, tc.opts...)
			var names []string
			for iter.Next() {
				names = append(names, iter.Object().Name())
			}
			if err := iter.Err(); err != nil {
				t.Fatal(err)
			}
			if len(names) != 101 || names[0] != "file-001" || names[99] != "file-100" || names[100] != "file-101" {
				t.Fatalf("listed names = %d (%q, %q, %q), want 101 fixed names", len(names), names[0], names[99], names[100])
			}
			calls := srv.Calls("b2_list_file_names")
			if len(calls) != 2 {
				t.Fatalf("listing requests = %d, want 2", len(calls))
			}
			for _, call := range calls {
				b2wiretest.AssertJSON(t, call, "maxFileCount", 100)
			}
			b2wiretest.AssertNoJSON(t, calls[0], "startFileName")
			b2wiretest.AssertJSON(t, calls[1], "startFileName", "file-101")
		})
	}
}

func TestListDefaultPageSizeOnWire(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-file-names
	// https://www.backblaze.com/apidocs/b2-list-file-versions
	for _, tc := range []struct {
		name      string
		call      string
		opts      []ListOption
		wantField bool
	}{
		{"names", "b2_list_file_names", nil, true},
		{"names explicit zero", "b2_list_file_names", []ListOption{ListPageSize(0)}, true},
		{"versions", "b2_list_file_versions", []ListOption{ListHidden()}, true},
		{"versions explicit zero", "b2_list_file_versions", []ListOption{ListHidden(), ListPageSize(0)}, true},
		{"unfinished", "b2_list_unfinished_large_files", []ListOption{ListUnfinished()}, false},
		{"unfinished explicit zero", "b2_list_unfinished_large_files", []ListOption{ListUnfinished(), ListPageSize(0)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Reply(tc.call, http.StatusOK, `{"files":[],"nextFileName":null,"nextFileId":null}`)
			bucket := newListingWireBucket(t, ctx, srv)
			iter := bucket.List(ctx, tc.opts...)
			if iter.Next() || iter.Err() != nil {
				t.Fatalf("empty listing returned an object or error: %v", iter.Err())
			}
			calls := srv.Calls(tc.call)
			if len(calls) != 1 {
				t.Fatalf("%s requests = %d, want 1", tc.call, len(calls))
			}
			if tc.wantField {
				b2wiretest.AssertJSON(t, calls[0], "maxFileCount", 100)
			} else {
				b2wiretest.AssertNoJSON(t, calls[0], "maxFileCount")
			}
		})
	}
}

func TestListPageSizeB2MaximumOnWire(t *testing.T) {
	// Both file listing APIs document 10,000 as the maximum maxFileCount.
	// https://www.backblaze.com/apidocs/b2-list-file-names
	// https://www.backblaze.com/apidocs/b2-list-file-versions
	for _, endpoint := range []struct {
		name string
		call string
		opts []ListOption
	}{
		{"names", "b2_list_file_names", nil},
		{"versions", "b2_list_file_versions", []ListOption{ListHidden()}},
	} {
		for _, tc := range []struct{ given, want int }{
			{1000, 1000}, {1001, 1001}, {9999, 9999}, {10000, 10000}, {10001, 10000},
		} {
			t.Run(fmt.Sprintf("%s/%d", endpoint.name, tc.given), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				srv := b2wiretest.New(t)
				srv.Reply(endpoint.call, http.StatusOK, `{"files":[],"nextFileName":null,"nextFileId":null}`)
				bucket := newListingWireBucket(t, ctx, srv)
				opts := append(append([]ListOption(nil), endpoint.opts...), ListPageSize(tc.given))
				iter := bucket.List(ctx, opts...)
				if iter.Next() || iter.Err() != nil {
					t.Fatalf("empty listing returned an object or error: %v", iter.Err())
				}
				calls := srv.Calls(endpoint.call)
				if len(calls) != 1 {
					t.Fatalf("%s requests = %d, want 1", endpoint.call, len(calls))
				}
				b2wiretest.AssertJSON(t, calls[0], "maxFileCount", tc.want)
			})
		}
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

// A version cursor can identify the next folder by name without a file ID.
func TestListVersionsContinuesWithNameOnlyCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := b2wiretest.New(t)
	const call = "b2_list_file_versions"
	pages := [2]string{
		`{"files":[{"fileName":"logs/a","fileId":"id-a","action":"upload"}],"nextFileName":"logs/sub/","nextFileId":null}`,
		`{"files":[{"fileName":"logs/sub/","fileId":null,"action":"folder"},{"fileName":"logs/z","fileId":"id-z","action":"upload"}],"nextFileName":null,"nextFileId":null}`,
	}
	srv.Handle(call, listingPages(pages, map[string]string{"startFileName": "logs/sub/"}))
	bucket := newListingWireBucket(t, ctx, srv)
	iter := bucket.List(ctx, ListHidden(), ListPrefix("logs/"), ListDelimiter("/"), ListPageSize(2))
	var names []string
	for iter.Next() {
		names = append(names, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("listing: %v", err)
	}
	if want := []string{"logs/a", "logs/z"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
	calls := srv.Calls(call)
	if len(calls) != 2 {
		t.Fatalf("%s requests = %d, want 2", call, len(calls))
	}
	b2wiretest.AssertNoJSON(t, calls[0], "startFileName")
	b2wiretest.AssertJSON(t, calls[1], "startFileName", "logs/sub/")
	for _, call := range calls {
		b2wiretest.AssertNoJSON(t, call, "startFileId")
		b2wiretest.AssertJSON(t, call, "prefix", "logs/")
		b2wiretest.AssertJSON(t, call, "delimiter", "/")
	}
}

// Listing unfinished uploads with a name prefix must only return
// uploads that match the prefix.
func TestRegression_ListUnfinishedHonoursPrefix(t *testing.T) {
	srv := b2wiretest.New(t)
	bucket := newListingWireBucket(t, newTestCtx(t), srv)
	all := []string{"logs/a", "other/b", "logs/c", "tmp/d"}
	srv.Handle("b2_list_unfinished_large_files", func(r b2wiretest.Request) b2wiretest.Response {
		prefix, _ := r.JSON["namePrefix"].(string) // a server honours the B2 namePrefix parameter
		var files []string
		for _, n := range all {
			if strings.HasPrefix(n, prefix) {
				files = append(files, `{"fileName":"`+n+`","fileId":"id-`+n+`","action":"start"}`)
			}
		}
		return b2wiretest.Response{Body: `{"files":[` + strings.Join(files, ",") + `],"nextFileId":null}`}
	})
	iter := bucket.List(newTestCtx(t), ListUnfinished(), ListPrefix("logs/"))
	var got []string
	for iter.Next() {
		got = append(got, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	// The fixture holds logs/a, logs/c, other/b and tmp/d; exactly the two
	// matching uploads must come back (the fake server lists in fixture
	// order, so the order is fixed), and the prefix must reach the wire.
	if want := []string{"logs/a", "logs/c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListUnfinished with ListPrefix(\"logs/\") returned %q, want %q", got, want)
	}
	calls := srv.Calls("b2_list_unfinished_large_files")
	if len(calls) == 0 {
		t.Fatal("no b2_list_unfinished_large_files request was sent")
	}
	for _, c := range calls {
		b2wiretest.AssertJSON(t, c, "namePrefix", "logs/")
	}
}

func TestListUnfinishedSendsCursorOnEveryPage(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-unfinished-large-files
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
	iter := bucket.List(ctx, ListUnfinished(), ListPrefix("logs/"), ListPageSize(2))
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
		b2wiretest.AssertJSON(t, r, "namePrefix", "logs/")
	}
	b2wiretest.AssertNoJSON(t, calls[0], "startFileId")
	b2wiretest.AssertJSON(t, calls[1], "startFileId", "id-c")
}

func TestListedObjectAttrsStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		file   string
		call   string
		opts   []ListOption
		action string
		want   ObjectState
	}{
		{
			name:   "unfinished large file",
			file:   "unfinished/large",
			call:   "b2_list_unfinished_large_files",
			opts:   []ListOption{ListPrefix("unfinished/"), ListUnfinished()},
			action: "start",
			want:   Started,
		},
		{
			name:   "current file",
			file:   "uploaded/file",
			call:   "b2_list_file_names",
			opts:   []ListOption{ListPrefix("uploaded/")},
			action: "upload",
			want:   Uploaded,
		},
		{
			name:   "hidden file",
			file:   "hidden/file",
			call:   "b2_list_file_versions",
			opts:   []ListOption{ListPrefix("hidden/"), ListHidden()},
			action: "hide",
			want:   Hider,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			srv := b2wiretest.New(t)
			srv.Reply(test.call, http.StatusOK, `{"files":[{"fileName":"`+test.file+`","fileId":"id","action":"`+test.action+`"}],"nextFileName":null,"nextFileId":null}`)
			bucket := newListingWireBucket(t, ctx, srv)

			iter := bucket.List(ctx, test.opts...)
			if !iter.Next() {
				t.Fatalf("listing yielded no object: %v", iter.Err())
			}
			attrs, err := iter.Object().Attrs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if attrs.Status != test.want {
				t.Errorf("Attrs().Status = %v, want %v", attrs.Status, test.want)
			}
			if iter.Next() {
				t.Fatal("listing yielded more than one object")
			}
			if err := iter.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
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
