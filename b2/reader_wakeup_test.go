package b2

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// wakeRoot fails every download after a small, varying delay so the fetch
// worker's setErr+Broadcast races the curChunk waiter's predicate check.
type wakeRoot struct {
	*testRoot
	calls int32
}

func (r *wakeRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, cors []CORSRule, fileLock bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, cors, fileLock)
	if err != nil {
		return nil, err
	}
	return &wakeBucket{testBucket: b.(*testBucket), r: r}, nil
}

type wakeBucket struct {
	*testBucket
	r *wakeRoot
}

func (b *wakeBucket) downloadFileByName(context.Context, string, int64, int64, bool) (b2FileReaderInterface, error) {
	n := atomic.AddInt32(&b.r.calls, 1)
	for spin := time.Now().Add(time.Duration(n%40) * time.Microsecond); time.Now().Before(spin); {
	}
	return nil, errors.New("injected download failure")
}

// Racy by nature: reliably red on v0.8.0 only under -race, which widens the
// window between the waiter's predicate check and its registration in Wait.
//
//	go test -race -run TestReaderCurChunkWaitersDoNotLeak ./b2/
func TestReaderCurChunkWaitersDoNotLeak(t *testing.T) {
	root := &wakeRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(context.Background(), bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	const readers = 20000
	for i := 0; i < readers; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		r := bucket.Object("obj").NewReader(ctx)
		_, _ = r.Read(make([]byte, 10))
		r.Close()
		cancel()
	}
	time.Sleep(500 * time.Millisecond)
	buf := make([]byte, 1<<24)
	if n := strings.Count(string(buf[:runtime.Stack(buf, true)]), "(*Reader).curChunk.func1"); n > 0 {
		t.Fatalf("goroutine leak: %d curChunk waiter goroutine(s) still parked after %d closed readers", n, readers)
	}
}
