package consistent

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
	"github.com/Backblaze/blazer/x/b2sim"
)

type failingWriterTo struct{ err error }

func (f failingWriterTo) Read([]byte) (int, error) { return 0, f.err }
func (f failingWriterTo) WriteTo(w io.Writer) (int64, error) {
	n, err := io.WriteString(w, "partial")
	if err != nil {
		return int64(n), err
	}
	return int64(n), f.err
}

func TestOperateStreamCopyFailureDoesNotPublishPartialWriterTo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(b2sim.New()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "consistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	srcErr := errors.New("callback output failed")
	err = NewGroup(bucket, "consistent").OperateStream(ctx, "obj", func(io.Reader) (io.Reader, error) {
		return failingWriterTo{err: srcErr}, nil
	})
	if !errors.Is(err, srcErr) {
		t.Fatalf("OperateStream err = %v, want %v", err, srcErr)
	}
	if n := len(client.Status().Writers); n != 0 {
		t.Errorf("%d writer(s) left open after copy failed", n)
	}
	iter := bucket.List(ctx, b2.ListHidden())
	if iter.Next() {
		t.Errorf("partial object was published: %s", iter.Object().Name())
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("list objects: %v", err)
	}
}

func newSimGroup(t *testing.T) (*b2.Client, *Group, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, err := b2.NewClient(ctx, "test-id", "test-key", b2.Transport(b2sim.New()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "consistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	return client, NewGroup(bucket, "consistent"), ctx
}

type failingSourceReader struct {
	data []byte
	err  error
}

func (f *failingSourceReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// A failed copy must release the registered writer.
func TestRegression_OperateStreamClosesWriterWhenCopyFails(t *testing.T) {
	client, g, ctx := newSimGroup(t)
	srcErr := errors.New("callback output failed")
	err := g.OperateStream(ctx, "obj", func(io.Reader) (io.Reader, error) {
		return &failingSourceReader{data: []byte("abc"), err: srcErr}, nil
	})
	if !errors.Is(err, srcErr) {
		t.Fatalf("OperateStream err = %v, want %v", err, srcErr)
	}
	if n := len(client.Status().Writers); n != 0 {
		t.Errorf("%d writer(s) left open after OperateStream failed to copy", n)
	}
}
