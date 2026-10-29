package b2

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// WriteTo reports completion even when the destination rejects the delayed
// Write, in which case io.Copy never calls Read a second time.
type blockedCopyReader struct {
	entered chan struct{}
	release chan struct{}
	done    chan struct{}
	read    bool
	n       int64
	err     error
}

func (r *blockedCopyReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	close(r.entered)
	<-r.release
	return copy(p, "payload"), nil
}

func (r *blockedCopyReader) WriteTo(w io.Writer) (int64, error) {
	defer close(r.done)
	r.n, r.err = io.Copy(w, struct{ io.Reader }{r})
	return r.n, r.err
}

func newBlockedCopyReader(t *testing.T) (*blockedCopyReader, func()) {
	t.Helper()
	r := &blockedCopyReader{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	var once sync.Once
	release := func() { once.Do(func() { close(r.release) }) }
	t.Cleanup(release)
	return r, release
}

type copyRecordingWriter struct{ calls int }

func (w *copyRecordingWriter) Write(p []byte) (int, error) {
	w.calls++
	return len(p), nil
}

func TestCopyContextCancellationStopsDelayedWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	source, release := newBlockedCopyReader(t)
	destination := &copyRecordingWriter{}
	done := make(chan error, 1)
	go func() {
		_, err := copyContext(ctx, destination, source)
		done <- err
	}()
	select {
	case <-source.entered:
	case <-wait.Done():
		t.Fatal("copy did not enter the blocked Read")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("copyContext error = %v, want context.Canceled", err)
		}
	case <-wait.Done():
		t.Fatal("copyContext waited for the blocked Read after cancellation")
	}
	release()
	select {
	case <-source.done:
	case <-wait.Done():
		t.Fatal("copy goroutine did not finish after releasing the Read")
	}
	if destination.calls != 0 {
		t.Errorf("destination Write calls after copyContext returned = %d, want 0", destination.calls)
	}
}

func TestWriterReadFromCancellationStopsWriteAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	client := &Client{backend: &beRoot{b2i: &testRoot{
		bucketMap: make(map[string]map[string]string),
		errs:      &errCont{},
	}}}
	bucket, err := client.NewBucket(ctx, "copy-context", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	writer := bucket.Object("cancelled").NewWriter(ctx)
	// Initialize the Writer so Close releases the same buffer a late Write
	// would otherwise touch. No upload is needed to reach the cancelled path.
	writer.init()
	source, release := newBlockedCopyReader(t)
	done := make(chan error, 1)
	go func() {
		_, err := writer.ReadFrom(source)
		done <- err
	}()
	select {
	case <-source.entered:
	case <-wait.Done():
		t.Fatal("ReadFrom did not enter the non-seekable reader")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadFrom error = %v, want context.Canceled", err)
		}
	case <-wait.Done():
		t.Fatal("ReadFrom waited for the blocked Read after cancellation")
	}
	if err := writer.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v, want context.Canceled", err)
	}
	release()
	select {
	case <-source.done:
	case <-wait.Done():
		t.Fatal("copy goroutine did not finish after Writer.Close")
	}
	// ErrClosed or context.Canceled here would come from Writer.Write itself:
	// the abandoned copy must reject the call before it can reach that method.
	if source.n != 0 || source.err == nil || source.err.Error() != "b2: copy abandoned" {
		t.Errorf("delayed Write after Close = (%d, %v), want (0, b2: copy abandoned)", source.n, source.err)
	}
}
