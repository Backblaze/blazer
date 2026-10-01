package b2

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// truncRoot serves every download one byte short of the size it reports,
// the shape of a connection that is reset just before the body completes.
type truncRoot struct {
	*testRoot
	calls int32
}

func (r *truncRoot) createBucket(ctx context.Context, name, btype string, info map[string]string, rules []LifecycleRule, sse *ServerSideEncryption) (b2BucketInterface, error) {
	b, err := r.testRoot.createBucket(ctx, name, btype, info, rules, sse)
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
	atomic.AddInt32(&b.r.calls, 1)
	gmux.Lock()
	f := b.files[name]
	gmux.Unlock()
	end := int(offset + size)
	if end > len(f) {
		end = len(f)
	}
	if int(offset) >= len(f) {
		return nil, errNoMoreContent
	}
	body := f[offset:end]
	return &testFileReader{
		b: ioutil.NopCloser(strings.NewReader(body[:len(body)-1])), // one byte short...
		s: end - int(offset),                                       // ...of the size reported
		n: name,
	}, nil
}

func TestReaderGivesUpOnPersistentlyTruncatedChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	root := &truncRoot{testRoot: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}
	bucket, err := (&Client{backend: &beRoot{b2i: root}}).NewBucket(ctx, bucketName, &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	gmux.Lock()
	root.bucketMap[bucketName]["obj"] = strings.Repeat("x", 1000)
	gmux.Unlock()

	r := bucket.Object("obj").NewReader(ctx)
	defer r.Close()
	_, err = io.ReadAll(r)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded retry: %d download attempts for one truncated chunk, ended only by the caller's context deadline",
			atomic.LoadInt32(&root.calls))
	}
	if err == nil {
		t.Fatal("expected an error for a chunk that is always truncated")
	}
}
