package b2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Credential-free fakes accept this below-minimum part size to force multipart uploads.
const readFromTestChunkSize = 3

func TestWriterReadFromReturnsUploadError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seekable bool
		resume   bool
	}{
		{name: "non-seekable"},
		{name: "seekable resume", seekable: true, resume: true},
		{name: "seekable streaming", seekable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket, srv := newMappingBucket(t, updateBucketJSON)
			srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[],"nextFileId":null}`)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
			srv.Reply("upload_part", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"part failed"}`)

			writer := bucket.Object("failed").NewWriter(context.Background())
			writer.ChunkSize = readFromTestChunkSize
			writer.ConcurrentUploads = 1
			writer.Resume = tc.resume
			payload := bytes.Repeat([]byte("x"), 128)
			var source io.Reader
			if tc.seekable {
				source = struct{ io.ReadSeeker }{bytes.NewReader(payload)}
			} else {
				source = io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
			}
			_, err := io.Copy(writer, source)
			if err == nil || err.Error() != "b2_upload_part: 400: part failed" {
				t.Errorf("io.Copy error = %v, want the permanent upload-part failure", err)
			}
			if got := len(srv.Calls("upload_part")); got == 0 {
				t.Error("no upload_part request reached the server")
			}
			if closeErr := writer.Close(); closeErr == nil || closeErr.Error() != "b2_upload_part: 400: part failed" {
				t.Errorf("Close error = %v, want the permanent upload-part failure", closeErr)
			}
		})
	}
}

func TestWriterReadFromCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deadline bool
		want     error
	}{
		{name: "cancel", want: context.Canceled},
		{name: "deadline", deadline: true, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.deadline {
				ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			bucket := newProgressReviewBucket(t)
			writer := bucket.Object("caller-context").NewWriter(ctx)
			source, release := newBlockedCopyReader(t)
			t.Cleanup(func() {
				release()
				select {
				case <-source.done:
				case <-time.After(5 * time.Second):
					t.Error("copy goroutine did not finish")
				}
				_ = writer.Close()
			})
			done := make(chan error, 1)
			go func() {
				_, err := io.Copy(writer, source)
				done <- err
			}()
			select {
			case <-source.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("copy did not enter the blocked Read")
			}
			if !tc.deadline {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, tc.want) {
					t.Fatalf("io.Copy error = %v, want %v", err, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("copy did not return after caller context ended")
			}
			if err := writer.getErr(); err != nil {
				t.Fatalf("Writer recorded error = %v, want nil", err)
			}
		})
	}
}

func TestWriterReadFromReturnsInjectedUploadError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seekable bool
		resume   bool
	}{
		{name: "non-seekable"},
		{name: "seekable resume", seekable: true, resume: true},
		{name: "seekable streaming", seekable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			injected := errors.New("permanent part upload failure")
			bucket := newProgressReviewBucket(t)
			bucket.b = &readFromFailureBucket{beBucketInterface: bucket.b, failure: injected}
			writer := bucket.Object("injected-error").NewWriter(context.Background())
			writer.ChunkSize = readFromTestChunkSize
			writer.ConcurrentUploads = 1
			writer.Resume = tc.resume
			payload := bytes.Repeat([]byte("x"), 128)
			var source io.Reader
			if tc.seekable {
				source = struct{ io.ReadSeeker }{bytes.NewReader(payload)}
			} else {
				source = io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
			}
			_, err := io.Copy(writer, source)
			if !errors.Is(err, injected) {
				t.Errorf("io.Copy error = %v, want injected upload error", err)
			}
			if err := writer.Close(); !errors.Is(err, injected) {
				t.Errorf("Close error = %v, want injected upload error", err)
			}
		})
	}
}

// A transport can flatten a caller-cancelled request into a non-context error.
// Hold the streaming producer until that error is recorded, then let it observe
// the cancelled context. The caller's cancellation must still win.
func TestWriterReadFromStreamingCallerContextWithRecordedTransportError(t *testing.T) {
	customCause := errors.New("caller-provided cancellation cause")
	for _, tc := range []struct {
		name        string
		deadline    bool
		customCause bool
		want        error
	}{
		{name: "cancel", want: context.Canceled},
		{name: "deadline", deadline: true, want: context.DeadlineExceeded},
		{name: "cancel with cause", customCause: true, want: context.Canceled},
		{name: "deadline with cause", deadline: true, customCause: true, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			var cancelWithCause context.CancelCauseFunc
			if tc.deadline {
				var cause error
				if tc.customCause {
					cause = customCause
				}
				delayed := newDelayedTimeoutCauseContext(100*time.Millisecond, cause)
				ctx, cancel = delayed, delayed.cancel
			} else if tc.customCause {
				ctx, cancelWithCause = context.WithCancelCause(context.Background())
				cancel = func() { cancelWithCause(nil) }
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()

			startEntered := make(chan struct{})
			releaseStart := make(chan struct{})
			partEntered := make(chan struct{})
			bufferEntered := make(chan struct{})
			releaseBuffer := make(chan struct{})
			recorded := make(chan struct{})
			flattened := errors.New("flattened transport error")
			bucket := newProgressReviewBucket(t)
			bucket.b = &readFromFailureBucket{
				beBucketInterface: bucket.b,
				startEntered:      startEntered,
				releaseStart:      releaseStart,
				partEntered:       partEntered,
				failure:           flattened,
			}
			writer := bucket.Object("caller-context-streaming").NewWriter(ctx,
				WithCancelOnError(func() context.Context { return context.Background() }, func(error) { close(recorded) }))
			writer.ChunkSize = readFromTestChunkSize
			writer.ConcurrentUploads = 1
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				_, err := io.Copy(writer, struct{ io.ReadSeeker }{bytes.NewReader(bytes.Repeat([]byte("x"), 128))})
				done <- err
			}()
			var releaseStartOnce, releaseBufferOnce sync.Once
			unblock := func() {
				releaseStartOnce.Do(func() { close(releaseStart) })
				releaseBufferOnce.Do(func() { close(releaseBuffer) })
			}
			t.Cleanup(func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("streaming copy did not finish")
				}
				_ = writer.Close()
			})
			waitReadFromEvent(t, startEntered, "large-file start")
			originalBuffer := writer.newBuffer
			writer.newBuffer = func() (writeBuffer, error) {
				close(bufferEntered)
				<-releaseBuffer
				return originalBuffer()
			}
			releaseStartOnce.Do(func() { close(releaseStart) })
			waitReadFromEvent(t, partEntered, "part upload")
			waitReadFromEvent(t, bufferEntered, "next buffer")
			if tc.deadline {
				delayed := ctx.(*delayedTimeoutCauseContext)
				delayed.start()
			} else {
				if tc.customCause {
					cancelWithCause(customCause)
				} else {
					cancel()
				}
			}
			waitReadFromEvent(t, recorded, "recorded transport error")
			if err := writer.getErr(); !errors.Is(err, flattened) {
				t.Fatalf("recorded error = %v, want flattened transport error", err)
			}
			releaseBufferOnce.Do(func() { close(releaseBuffer) })
			select {
			case err := <-done:
				if !errors.Is(err, tc.want) {
					t.Fatalf("io.Copy error = %v, want caller's %v", err, tc.want)
				}
				if tc.customCause && errors.Is(err, customCause) {
					t.Fatalf("io.Copy error = %v, unexpectedly matches caller's custom cause", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("streaming copy did not return")
			}
		})
	}
}

func waitReadFromEvent(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

type readFromFailureBucket struct {
	beBucketInterface
	startEntered, releaseStart, partEntered chan struct{}
	failure                                 error
}

func (b *readFromFailureBucket) startLargeFile(ctx context.Context, name, contentType string, info map[string]string, retention *FileRetention, legalHold LegalHold, encryption *uploadEncryption) (beLargeFileInterface, error) {
	file, err := b.beBucketInterface.startLargeFile(ctx, name, contentType, info, retention, legalHold, encryption)
	if err != nil {
		return nil, err
	}
	if b.startEntered != nil {
		close(b.startEntered)
		<-b.releaseStart
	}
	return &readFromFailureFile{beLargeFileInterface: file, partEntered: b.partEntered, failure: b.failure}, nil
}

func (b *readFromFailureBucket) listUnfinishedLargeFiles(context.Context, int, string) ([]beFileInterface, string, error) {
	return nil, "", nil
}

type readFromFailureFile struct {
	beLargeFileInterface
	partEntered chan struct{}
	failure     error
}

func (f *readFromFailureFile) getUploadPartURL(ctx context.Context) (beFileChunkInterface, error) {
	chunk, err := f.beLargeFileInterface.getUploadPartURL(ctx)
	if err != nil {
		return nil, err
	}
	return &readFromFailureChunk{beFileChunkInterface: chunk, partEntered: f.partEntered, failure: f.failure}, nil
}

type readFromFailureChunk struct {
	beFileChunkInterface
	partEntered chan struct{}
	failure     error
}

func (c *readFromFailureChunk) uploadPart(ctx context.Context, _ readResetter, _ string, _, _ int) (int, error) {
	if c.partEntered != nil {
		close(c.partEntered)
		<-ctx.Done()
	}
	return 0, c.failure
}

// delayedTimeoutCauseContext starts its timeout only after a test has reached
// the controlled upload state. Value forwards the timed context's cause to
// Writer's derived context.
type delayedTimeoutCauseContext struct {
	mu       sync.Mutex
	ctx      context.Context
	timeout  time.Duration
	cause    error
	done     chan struct{}
	err      error
	started  bool
	cancelFn context.CancelFunc
}

func newDelayedTimeoutCauseContext(timeout time.Duration, cause error) *delayedTimeoutCauseContext {
	return &delayedTimeoutCauseContext{
		ctx:     context.Background(),
		timeout: timeout,
		cause:   cause,
		done:    make(chan struct{}),
	}
}

func (c *delayedTimeoutCauseContext) Deadline() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx.Deadline()
}

func (c *delayedTimeoutCauseContext) Done() <-chan struct{} { return c.done }

func (c *delayedTimeoutCauseContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *delayedTimeoutCauseContext) Value(key any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx.Value(key)
}

func (c *delayedTimeoutCauseContext) start() {
	c.mu.Lock()
	if c.started || c.err != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeoutCause(context.Background(), c.timeout, c.cause)
	c.ctx, c.cancelFn, c.started = ctx, cancel, true
	c.mu.Unlock()
	go func() {
		<-ctx.Done()
		c.finish(ctx.Err())
	}()
}

func (c *delayedTimeoutCauseContext) cancel() {
	c.mu.Lock()
	cancel := c.cancelFn
	c.mu.Unlock()
	if cancel != nil {
		cancel()
		return
	}
	c.finish(context.Canceled)
}

func (c *delayedTimeoutCauseContext) finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	close(c.done)
}
