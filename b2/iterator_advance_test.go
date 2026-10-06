package b2

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// scriptRoot wraps the stock fakes so a test can script individual listing
// pages. A page func returns handled=false to fall through to the stock fake.
type scriptRoot struct {
	*testRoot
	names      func(call int, cont string) (fs []b2FileInterface, next string, handled bool)
	unfinished func(call int, cont string) (fs []b2FileInterface, next string, handled bool)
	calls      int32
}

func (r *scriptRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled)
	if err != nil {
		return nil, err
	}
	return &scriptBucket{testBucket: b.(*testBucket), r: r}, nil
}

type scriptBucket struct {
	*testBucket
	r *scriptRoot
}

func (b *scriptBucket) listFileNames(ctx context.Context, count int, cont, pfx, del string) ([]b2FileInterface, string, error) {
	n := int(atomic.AddInt32(&b.r.calls, 1))
	if b.r.names != nil {
		if fs, next, ok := b.r.names(n, cont); ok {
			return fs, next, nil
		}
	}
	return b.testBucket.listFileNames(ctx, count, cont, pfx, del)
}

func (b *scriptBucket) listUnfinishedLargeFiles(ctx context.Context, count int, cont string) ([]b2FileInterface, string, error) {
	n := int(atomic.AddInt32(&b.r.calls, 1))
	if b.r.unfinished != nil {
		if fs, next, ok := b.r.unfinished(n, cont); ok {
			return fs, next, nil
		}
	}
	return b.testBucket.listUnfinishedLargeFiles(ctx, count, cont)
}

func scriptedBucket(t *testing.T, root *scriptRoot) (*Bucket, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	gmux.Lock()
	for _, n := range []string{"a", "b", "c"} {
		root.bucketMap[bucketName][n] = n
	}
	gmux.Unlock()
	return bucket, ctx
}

func newScriptRoot() *scriptRoot {
	return &scriptRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}
}

// A backend that keeps returning an empty page with the same cursor must make
// the iterator fail with an error, not hang or overflow the stack.
func TestListFailsWhenAnEmptyPageDoesNotAdvance(t *testing.T) {
	root := newScriptRoot()
	root.names = func(int, string) ([]b2FileInterface, string, bool) { return nil, "a", true }
	bucket, ctx := scriptedBucket(t, root)

	iter := bucket.List(ctx, ListPageSize(100))
	for iter.Next() {
	}
	err := iter.Err()
	if err == nil || !strings.Contains(err.Error(), "not advancing") {
		t.Fatalf("Err() = %v, want a 'not advancing' error", err)
	}
	if got := atomic.LoadInt32(&root.calls); got > 3 {
		t.Errorf("backend was asked %d times, want the listing to stop within a few requests", got)
	}
}

// Many consecutive empty pages that do advance are legitimate and must all be
// walked, without recursion.
func TestListWalksManyConsecutiveEmptyPages(t *testing.T) {
	const empties = 5000
	root := newScriptRoot()
	root.names = func(n int, cont string) ([]b2FileInterface, string, bool) {
		if n <= empties {
			return nil, fmt.Sprintf("%06d", n), true // digits sort before "a"
		}
		return nil, "", false
	}
	bucket, ctx := scriptedBucket(t, root)

	var got []string
	iter := bucket.List(ctx, ListPageSize(100))
	for iter.Next() {
		got = append(got, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("listed %v after %d empty pages, want [a b c]", got, empties)
	}
}

// listUnfinishedLargeFiles is the third lister the fix touches; it had no
// coverage for an empty page that carries a cursor.
func TestListUnfinishedContinuesPastEmptyPageWithCursor(t *testing.T) {
	root := newScriptRoot()
	root.unfinished = func(n int, cont string) ([]b2FileInterface, string, bool) {
		switch n {
		case 1:
			return nil, "next", true
		case 2:
			return []b2FileInterface{&testFile{n: "big", a: "start", files: map[string]string{}}}, "", true
		}
		return nil, "", true
	}
	bucket, ctx := scriptedBucket(t, root)

	var got []string
	iter := bucket.List(ctx, ListUnfinished(), ListPageSize(100))
	for iter.Next() {
		got = append(got, iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if len(got) != 1 || got[0] != "big" {
		t.Fatalf("listed %v, want [big]: an empty unfinished page with a cursor ended the listing", got)
	}
}
