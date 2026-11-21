package b2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

type readerStatusTimerRoot struct {
	beRootInterface
	after func(time.Duration) <-chan time.Time
}

func (r readerStatusTimerRoot) retryAfter(d time.Duration) <-chan time.Time {
	return r.after(d)
}

func assertSingleReaderProgress(t *testing.T, bucket *Bucket, want float64) {
	t.Helper()
	readers := bucket.c.Status().Readers
	if len(readers) != 1 {
		t.Fatalf("active readers = %d, want 1", len(readers))
	}
	for _, status := range readers {
		if got := status.Progress; len(got) != 1 || got[0] != want {
			t.Fatalf("reader progress = %v, want [%g]", got, want)
		}
	}
}

func TestReaderStatusDuringRetry(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	ctx := newTestCtx(t)
	waiting, retryHeaders := make(chan struct{}), make(chan struct{})
	tick, headers := make(chan time.Time), make(chan struct{})
	releaseTick := sync.OnceFunc(func() { close(tick) })
	releaseHeaders := sync.OnceFunc(func() { close(headers) })
	backoffEntered := sync.OnceFunc(func() { close(waiting) })
	bucket.r = readerStatusTimerRoot{bucket.r, func(time.Duration) <-chan time.Time {
		backoffEntered()
		return tick
	}}
	srv.ReplySequence("retry-status", b2wiretest.Response{
		Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {"retry-id"}}, Body: "abcd",
	})
	srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
		if id := req.Query.Get("fileId"); id != "retry-id" {
			t.Errorf("retry fileId = %q, want retry-id", id)
		}
		close(retryHeaders)
		<-headers
		return b2wiretest.Response{Header: http.Header{"X-Bz-File-Id": {"retry-id"}}, Body: "abcdefgh"}
	})
	reader := bucket.Object("retry-status").NewRangeReader(ctx, 0, 8)
	reader.ChunkSize, reader.ConcurrentDownloads = 8, 1
	t.Cleanup(func() { releaseTick(); releaseHeaders(); _ = reader.Close() })
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	go func() { body, err := io.ReadAll(reader); done <- result{body, err} }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("truncated download did not enter retry backoff")
	}
	assertSingleReaderProgress(t, bucket, 0)
	releaseTick()
	select {
	case <-retryHeaders:
	case <-ctx.Done():
		t.Fatal("retry did not reach its response-header wait")
	}
	assertSingleReaderProgress(t, bucket, 0)
	releaseHeaders()
	select {
	case got := <-done:
		if got.err != nil || string(got.body) != "abcdefgh" {
			t.Fatalf("retried read = %q, %v; want abcdefgh, nil", got.body, got.err)
		}
	case <-ctx.Done():
		t.Fatal("retried download did not complete")
	}
	assertSingleReaderProgress(t, bucket, 1)
	if names, ids := len(srv.Calls("retry-status")), len(srv.Calls("b2_download_file_by_id")); names != 1 || ids != 1 {
		t.Errorf("download calls by name/ID = %d/%d, want 1/1", names, ids)
	}
}

func TestReaderStatusAfterRetryExhaustion(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	ready := make(chan time.Time)
	close(ready)
	bucket.r = readerStatusTimerRoot{bucket.r, func(time.Duration) <-chan time.Time { return ready }}
	short := b2wiretest.Response{Header: http.Header{"Content-Length": {"8"}, "X-Bz-File-Id": {"retry-id"}}, Body: "abcd"}
	srv.ReplySequence("retry-exhausted", short)
	srv.Handle("b2_download_file_by_id", func(req b2wiretest.Request) b2wiretest.Response {
		if id := req.Query.Get("fileId"); id != "retry-id" {
			t.Errorf("retry fileId = %q, want retry-id", id)
		}
		return short
	})
	reader := bucket.Object("retry-exhausted").NewRangeReader(newTestCtx(t), 0, 8)
	reader.ChunkSize, reader.ConcurrentDownloads = 8, 1
	t.Cleanup(func() { _ = reader.Close() })
	body, err := io.ReadAll(reader)
	if len(body) != 0 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("exhausted read = %q, %v; want no bytes and unexpected EOF", body, err)
	}
	if calls := len(srv.Calls("retry-exhausted")) + len(srv.Calls("b2_download_file_by_id")); calls != maxShortReadAttempts {
		t.Errorf("download attempts = %d, want %d", calls, maxShortReadAttempts)
	}
	assertSingleReaderProgress(t, bucket, 0)
}

type readerStatusFaultBody struct{ err error }

func (r readerStatusFaultBody) Read(p []byte) (int, error) {
	return copy(p, "abcdefgh"), r.err
}

type readerStatusFaultBucket struct {
	beBucketInterface
	err error
}

func (b readerStatusFaultBucket) downloadFileByName(context.Context, string, int64, int64, bool, *SSECKey) (beFileReaderInterface, error) {
	return &testFileReader{b: io.NopCloser(readerStatusFaultBody{b.err}), s: 8, n: "failed-id"}, nil
}

func TestReaderStatusAfterBodyError(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	want := errors.New("download body failed")
	bucket.b = readerStatusFaultBucket{bucket.b, want}
	reader := bucket.Object("failed").NewRangeReader(newTestCtx(t), 0, 8)
	t.Cleanup(func() { _ = reader.Close() })
	body, err := io.ReadAll(reader)
	if len(body) != 0 || !errors.Is(err, want) {
		t.Fatalf("failed read = %q, %v; want no bytes and the body error", body, err)
	}
	assertSingleReaderProgress(t, bucket, 0)
}

// truncRoot serves a download one byte short of the size it reports, the shape
// of a connection reset just before the body completes. The first truncate
// downloads are cut short (every one if truncate is negative); later ones are
// served whole. Sleeps between attempts return at once.
type truncRoot struct {
	*testRoot
	truncate int32
	calls    int32
}

func (r *truncRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool, replicationConfig *ReplicationConfiguration) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled, replicationConfig)
	if err != nil {
		return nil, err
	}
	return &truncBucket{testBucket: b.(*testBucket), r: r}, nil
}

type truncBucket struct {
	*testBucket
	r *truncRoot
}

func (b *truncBucket) downloadFileByName(_ context.Context, name string, offset, size int64, _ bool, _ *SSECKey) (b2FileReaderInterface, error) {
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

func (b *truncBucket) downloadFileByID(ctx context.Context, id string, offset, size int64, header bool, ssec *SSECKey) (b2FileReaderInterface, error) {
	return b.downloadFileByName(ctx, id, offset, size, header, ssec)
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
	got, calls, err := readTruncated(t, 20)
	if err != nil {
		t.Fatalf("a chunk truncated 20 times before being served whole failed after %d attempts: %v", calls, err)
	}
	if got != strings.Repeat("x", 1000) {
		t.Errorf("content after 20 truncated downloads = %q, want 1000 x bytes", got)
	}
	if calls != 21 {
		t.Errorf("download calls after 20 truncated downloads = %d, want 21", calls)
	}
	if _, _, err := readTruncated(t, 21); err == nil {
		t.Error("21 truncated downloads succeeded, want an error")
	}
}
