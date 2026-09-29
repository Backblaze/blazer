package b2

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// pageRoot wraps the stock fakes so the first listing page carries no usable
// objects but does carry a cursor to the next page -- a page B2's contract
// allows, since only a null next cursor means the listing is complete.
type pageRoot struct {
	*testRoot
	calls int32
}

func (r *pageRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, cors []CORSRule, fileLock bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, cors, fileLock)
	if err != nil {
		return nil, err
	}
	return &pageBucket{testBucket: b.(*testBucket), r: r}, nil
}

type pageBucket struct {
	*testBucket
	r *pageRoot
}

// First call: an empty page whose cursor points at "a". Then the real listing.
func (b *pageBucket) listFileNames(ctx context.Context, count int, cont, pfx, del string) ([]b2FileInterface, string, error) {
	if atomic.AddInt32(&b.r.calls, 1) == 1 {
		return nil, "a", nil
	}
	return b.testBucket.listFileNames(ctx, count, cont, pfx, del)
}

// First call: a page holding only an unfinished large file ("start"), which
// ListHidden filters out, plus a cursor to "a". Then the real listing.
func (b *pageBucket) listFileVersions(ctx context.Context, count int, name, id, pfx, del string) ([]b2FileInterface, string, string, error) {
	if atomic.AddInt32(&b.r.calls, 1) == 1 {
		return []b2FileInterface{&testFile{n: "0-in-progress", a: "start", files: b.files}}, "a", "a-id", nil
	}
	return b.testBucket.listFileVersions(ctx, count, name, id, pfx, del)
}

func listAfterEmptyFirstPage(t *testing.T, opts ...ListOption) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := &pageRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	gmux.Lock()
	for _, n := range []string{"a", "b", "c"} {
		root.bucketMap[bucketName][n] = n
	}
	gmux.Unlock()
	// An explicit page size: the stock fake returns nothing for the default of 0.
	opts = append(opts, ListPageSize(100))
	var got []string
	iter := bucket.List(ctx, opts...)
	for iter.Next() {
		got = append(got, iter.Object().Name())
	}
	return got, iter.Err()
}

func TestListContinuesPastEmptyPageWithCursor(t *testing.T) {
	got, err := listAfterEmptyFirstPage(t)
	if err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("silent truncation: listed %v (%d objects, Err() == nil); want [a b c] -- an empty page that carries a next cursor ended the listing", got, len(got))
	}
}

func TestListHiddenContinuesPastFilteredPage(t *testing.T) {
	got, err := listAfterEmptyFirstPage(t, ListHidden())
	if err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("silent truncation: listed %v (%d objects, Err() == nil); want [a b c] -- a page whose entries were all filtered out ended the listing", got, len(got))
	}
}
