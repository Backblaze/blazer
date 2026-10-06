package b2

import (
	"context"
	"io"
	"testing"
	"time"
)

// deadlockRoot wraps the stock fakes so every part upload is delayed. That
// lets a part fail while the producer is already blocked handing the next
// chunk to the (single) worker in sendChunk.
type deadlockRoot struct{ *testRoot }

func (r *deadlockRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption, corsRules []CORSRule, fileLockEnabled bool) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse, corsRules, fileLockEnabled)
	if err != nil {
		return nil, err
	}
	return &deadlockBucket{b.(*testBucket)}, nil
}

type deadlockBucket struct{ *testBucket }

func (b *deadlockBucket) startLargeFile(ctx context.Context, name, ct string, info map[string]string) (b2LargeFileInterface, error) {
	lf, err := b.testBucket.startLargeFile(ctx, name, ct, info)
	if err != nil {
		return nil, err
	}
	return &deadlockLargeFile{lf.(*testLargeFile)}, nil
}

type deadlockLargeFile struct{ *testLargeFile }

func (l *deadlockLargeFile) getUploadPartURL(ctx context.Context) (b2FileChunkInterface, error) {
	c, err := l.testLargeFile.getUploadPartURL(ctx)
	if err != nil {
		return nil, err
	}
	return &deadlockChunk{c.(*testFileChunk)}, nil
}

type deadlockChunk struct{ *testFileChunk }

func (c *deadlockChunk) uploadPart(ctx context.Context, r io.Reader, sha string, size, index int) (int, error) {
	time.Sleep(200 * time.Millisecond)
	return c.testFileChunk.uploadPart(ctx, r, sha, size, index)
}

func TestWriterPermanentPartFailureDoesNotDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &Client{backend: &beRoot{b2i: &deadlockRoot{&testRoot{
		bucketMap: make(map[string]map[string]string),
		errs: &errCont{errMap: map[string]map[int]error{
			"uploadPart": {0: testError{}}, // first part fails permanently
		}},
	}}}}
	bucket, err := client.NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("deadlock").NewWriter(ctx)
	w.ChunkSize = 1e4
	w.ConcurrentUploads = 1 // the default

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(w, io.LimitReader(zReader{}, 1e5))
		if err == nil {
			err = w.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the permanent part failure to be returned")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: io.Copy has not returned 5s after a permanent part-upload failure")
	}
}
