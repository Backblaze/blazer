package b2

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// wakeRoot serves downloads through failDownload, or fails them after a small,
// varying delay so a worker's setErr and broadcast race the curChunk waiter's
// predicate check. A download with block set waits for release instead of
// returning, so no worker reaches a broadcast on its own.
type wakeRoot struct {
	*testRoot
	calls   int32
	block   bool
	release chan struct{}
}

func (r *wakeRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled)
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
	if b.r.block {
		<-b.r.release
		return nil, errors.New("released")
	}
	for spin := time.Now().Add(time.Duration(n%40) * time.Microsecond); time.Now().Before(spin); {
	}
	return nil, errors.New("injected download failure")
}

func newWakeBucket(t *testing.T, block bool) (*Bucket, *wakeRoot) {
	t.Helper()
	root := &wakeRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}, block: block, release: make(chan struct{})}
	t.Cleanup(func() { close(root.release) })
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(context.Background(), bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket, root
}

// waitFor polls until the number of parked waiters across the readers equals
// want, or fails after the timeout.
func waitForWaiters(t *testing.T, want int32, timeout time.Duration, readers ...*Reader) {
	t.Helper()
	count := func() (n int32) {
		for _, r := range readers {
			n += atomic.LoadInt32(&r.waiters)
		}
		return n
	}
	deadline := time.Now().Add(timeout)
	for count() != want {
		if time.Now().After(deadline) {
			t.Fatalf("%d curChunk waiter(s) after %v, want %d", count(), timeout, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// readInBackground starts a Read that parks a curChunk waiter, and returns once
// the waiter is registered so the test is not vacuous.
func readInBackground(t *testing.T, r *Reader) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 10))
		done <- err
	}()
	waitForWaiters(t, 1, 5*time.Second, r)
	return done
}

// The caller cancelling the context, without Close, must unpark the waiter.
func TestCurChunkWaiterIsWokenWhenTheCallerCancels(t *testing.T) {
	bucket, _ := newWakeBucket(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	r := bucket.Object("obj").NewReader(ctx)
	done := readInBackground(t, r)

	cancel() // no Close
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Read error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not return after the context was cancelled")
	}
	waitForWaiters(t, 0, 5*time.Second, r)
}

func TestCurChunkWaiterIsWokenByClose(t *testing.T) {
	bucket, _ := newWakeBucket(t, true)
	r := bucket.Object("obj").NewReader(context.Background())
	done := readInBackground(t, r)

	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not return after Close")
	}
	waitForWaiters(t, 0, 5*time.Second, r)
}

// Close before any Read must not panic or race with the lazy initialization.
func TestCloseBeforeFirstRead(t *testing.T) {
	bucket, _ := newWakeBucket(t, false)
	r := bucket.Object("obj").NewReader(context.Background())
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Error("Read after Close succeeded, want an error")
	}
}

// Close used to read the lazily created condition variable while the first Read
// created it. Run under -race.
func TestCloseRacesWithTheFirstRead(t *testing.T) {
	bucket, _ := newWakeBucket(t, false)
	for i := 0; i < 200; i++ {
		r := bucket.Object("obj").NewReader(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Read(make([]byte, 1))
		}()
		_ = r.Close()
		wg.Wait()
	}
}

// Many readers whose workers fail while the waiter is checking its predicate:
// none may leave a waiter parked.
func TestCurChunkWaitersDoNotLeak(t *testing.T) {
	bucket, _ := newWakeBucket(t, false)
	const readers = 3000
	all := make([]*Reader, 0, readers)
	for i := 0; i < readers; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		r := bucket.Object("obj").NewReader(ctx)
		_, _ = r.Read(make([]byte, 10))
		_ = r.Close()
		cancel()
		all = append(all, r)
	}
	waitForWaiters(t, 0, 10*time.Second, all...)
}
