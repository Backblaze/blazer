package b2

import (
	"context"
	"io"
	"testing"
	"time"
)

// closeLeakRoot delays part uploads so Write returns before the first part
// fails; the failure then cancels the writer context before Close runs.
type closeLeakRoot struct{ *testRoot }

func (r *closeLeakRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse)
	if err != nil {
		return nil, err
	}
	return &closeLeakBucket{b.(*testBucket)}, nil
}

type closeLeakBucket struct{ *testBucket }

func (b *closeLeakBucket) startLargeFile(ctx context.Context, name, ct string, info map[string]string) (b2LargeFileInterface, error) {
	lf, err := b.testBucket.startLargeFile(ctx, name, ct, info)
	if err != nil {
		return nil, err
	}
	return &closeLeakLargeFile{lf.(*testLargeFile)}, nil
}

type closeLeakLargeFile struct{ *testLargeFile }

func (l *closeLeakLargeFile) getUploadPartURL(ctx context.Context) (b2FileChunkInterface, error) {
	c, err := l.testLargeFile.getUploadPartURL(ctx)
	if err != nil {
		return nil, err
	}
	return &closeLeakChunk{c.(*testFileChunk)}, nil
}

type closeLeakChunk struct{ *testFileChunk }

func (c *closeLeakChunk) uploadPart(ctx context.Context, r io.Reader, sha string, size, index int) (int, error) {
	time.Sleep(100 * time.Millisecond)
	return c.testFileChunk.uploadPart(ctx, r, sha, size, index)
}

func TestWriterCloseDrainsWorkersAfterFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &Client{backend: &beRoot{b2i: &closeLeakRoot{&testRoot{
		bucketMap: make(map[string]map[string]string),
		errs: &errCont{errMap: map[string]map[int]error{
			"uploadPart": {0: testError{}}, // first part fails permanently
		}},
	}}}}
	bucket, err := client.NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	w := bucket.Object("leak").NewWriter(ctx)
	w.ChunkSize = 1e4
	w.ConcurrentUploads = 2 // one worker fails, the other sits idle

	// One full chunk is handed to a worker (and fails); 100 bytes stay buffered for Close.
	if _, err := w.Write(make([]byte, 1e4+100)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); w.ctx.Err() == nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("setup: the failing worker never cancelled the writer context")
		}
	}
	if err := w.Close(); err == nil {
		t.Fatal("expected Close to return the failure")
	}

	drained := make(chan struct{})
	go func() { w.wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine leak: Close returned but an idle upload worker is still parked (w.wg never drained)")
	}
}
