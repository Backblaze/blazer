package b2

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// truncRoot serves a download one byte short of the size it reports, the shape
// of a connection reset just before the body completes. The first truncate
// downloads are cut short (every one if truncate is negative); later ones are
// served whole. Sleeps between attempts return at once.
type truncRoot struct {
	*testRoot
	truncate int32
	calls    int32
}

func (r *truncRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled)
	if err != nil {
		return nil, err
	}
	return &truncBucket{testBucket: b.(*testBucket), r: r}, nil
}

type truncBucket struct {
	*testBucket
	r *truncRoot
}

func (b *truncBucket) downloadFileByName(_ context.Context, name string, offset, size int64, _ bool) (b2FileReaderInterface, error) {
	gmux.Lock()
	f := b.files[name]
	gmux.Unlock()
	end := int(offset + size)
	if end > len(f) {
		end = len(f)
	}
	if int(offset) >= len(f) {
		return nil, errNoMoreContent // the reader's probe past the end; not a download
	}
	n := atomic.AddInt32(&b.r.calls, 1)
	body := f[offset:end]
	served := body
	if b.r.truncate < 0 || n <= b.r.truncate {
		served = body[:len(body)-1] // one byte short...
	}
	return &testFileReader{
		b: io.NopCloser(strings.NewReader(served)),
		s: end - int(offset), // ...of the size reported
		n: name,
	}, nil
}

// readTruncated reads a 1000-byte object whose chunk is truncated for the first
// truncate downloads, and returns what was read, the error, and how many
// downloads were attempted.
func readTruncated(t *testing.T, truncate int32) (string, int32, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	root := &truncRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}, truncate: truncate}
	root.afterFunc = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		close(ch)
		return ch
	}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("x", 1000)
	gmux.Lock()
	root.bucketMap[bucketName]["obj"] = want
	gmux.Unlock()

	r := bucket.Object("obj").NewReader(ctx)
	defer func() { _ = r.Close() }()
	got, err := io.ReadAll(r)
	return string(got), atomic.LoadInt32(&root.calls), err
}

// A chunk that always arrives truncated is retried a bounded number of times,
// and the error says why.
func TestReaderGivesUpOnPersistentlyTruncatedChunk(t *testing.T) {
	_, calls, err := readTruncated(t, -1)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded retry: %d download attempts, ended only by the caller's context deadline", calls)
	}
	if err == nil {
		t.Fatal("expected an error for a chunk that is always truncated")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("error = %v, want it to wrap io.ErrUnexpectedEOF so callers can identify it", err)
	}
	if calls != maxShortReadAttempts {
		t.Errorf("%d download attempts, want exactly %d", calls, maxShortReadAttempts)
	}
}

// A transient interruption that clears within the budget must not fail the read.
func TestReaderSurvivesTransientTruncation(t *testing.T) {
	for _, truncated := range []int32{1, 5, maxShortReadAttempts - 1} {
		got, calls, err := readTruncated(t, truncated)
		if err != nil {
			t.Fatalf("truncated %d times then served whole: %v", truncated, err)
		}
		if len(got) != 1000 {
			t.Errorf("truncated %d times: read %d bytes, want 1000", truncated, len(got))
		}
		if calls != truncated+1 {
			t.Errorf("truncated %d times: %d download attempts, want %d", truncated, calls, truncated+1)
		}
	}
}

// The boundary: one truncated download more than the budget fails.
func TestReaderBudgetBoundary(t *testing.T) {
	if _, _, err := readTruncated(t, maxShortReadAttempts-1); err != nil {
		t.Errorf("truncated %d times (one under the budget): %v", maxShortReadAttempts-1, err)
	}
	if _, _, err := readTruncated(t, maxShortReadAttempts); err == nil {
		t.Errorf("truncated %d times (the whole budget): the read succeeded, want the bounded error", maxShortReadAttempts)
	}
}

// base lets a download retry 20 times after its first attempt. A reader that gave
// up sooner would fail a read the rest of the library survives, so the budget is
// pinned: 20 truncated downloads followed by a good one must succeed.
func TestReaderBudgetMatchesTheDownloadRetryBudget(t *testing.T) {
	if _, calls, err := readTruncated(t, 20); err != nil {
		t.Fatalf("a chunk truncated 20 times before being served whole failed after %d attempts: %v", calls, err)
	}
}
