package b2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestWriterCloseWaitsForCancelDeadline(t *testing.T) {
	ctx := context.Background()
	cancelEntered := make(chan struct{})
	cancelRelease := make(chan struct{})
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
	srv.Reply("upload_part", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"part failed"}`)
	transport := bucketInfoRoundTripper(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/b2_cancel_large_file") {
			defer r.Body.Close()
			close(cancelEntered)
			select {
			case <-r.Context().Done():
			case <-cancelRelease:
			}
			return nil, r.Context().Err()
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "copy-deadline", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	callbackResult := make(chan error, 1)
	writer := bucket.Object("cancelled").NewWriter(ctx, WithCancelOnError(func() context.Context {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		t.Cleanup(cancel)
		return cleanupCtx
	}, func(err error) { callbackResult <- err }))
	// The recording server does not enforce B2's minimum part size.
	writer.ChunkSize = 3
	closed := make(chan error, 1)
	t.Cleanup(func() {
		close(cancelRelease)
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Error("Close goroutine did not finish during cleanup")
		}
	})
	// Leave the worker in the cancel RPC before asking Close to wait for it.
	if _, err := writer.Write([]byte("payload")); err == nil || !strings.Contains(err.Error(), "part failed") {
		t.Fatalf("Write error = %v, want the worker's part failed error", err)
	}
	select {
	case <-cancelEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter cancel_large_file")
	}
	go func() {
		closed <- writer.Close()
		close(closed)
	}()
	select {
	case err := <-closed:
		if err == nil || !strings.Contains(err.Error(), "part failed") {
			t.Fatalf("Close error = %v, want the recorded part failed error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited past the ctxf deadline for cancel_large_file")
	}
	select {
	case err := <-callbackResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("errf result = %v, want context.DeadlineExceeded", err)
		}
	default:
		t.Fatal("Close returned before errf recorded the cancel RPC's deadline error")
	}
}

// Retain the destination until after ReadFrom returns, then attempt a late Write.
type lateCopyWriteReader struct {
	release chan struct{}
	done    chan struct{}
	n       int
	err     error
}

func (*lateCopyWriteReader) Read([]byte) (int, error) { return 0, io.EOF }

func (r *lateCopyWriteReader) WriteTo(w io.Writer) (int64, error) {
	defer close(r.done)
	n, err := w.Write([]byte("payload"))
	<-r.release
	r.n, r.err = w.Write([]byte("late"))
	return int64(n), err
}

func TestWriterReadFromCancellationCancelsStartLargeFile(t *testing.T) {
	for _, fileBuffer := range []bool{false, true} {
		name := "memory"
		if fileBuffer {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			startEntered := make(chan struct{})
			startRelease := make(chan struct{})
			srv := b2wiretest.New(t)
			srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
			srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
			transport := bucketInfoRoundTripper(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/b2_start_large_file") {
					defer r.Body.Close()
					close(startEntered)
					select {
					case <-r.Context().Done():
					case <-startRelease:
					}
					return nil, r.Context().Err()
				}
				return http.DefaultTransport.RoundTrip(r)
			})
			client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(transport))
			if err != nil {
				t.Fatal(err)
			}
			bucket, err := client.NewBucket(ctx, "copy-start", &BucketAttrs{Type: Private})
			if err != nil {
				t.Fatal(err)
			}
			writer := bucket.Object("cancelled").NewWriter(ctx)
			writer.ChunkSize = 3
			writer.UseFileBuffer = fileBuffer
			writer.FileBufferDir = t.TempDir()
			source := &lateCopyWriteReader{release: make(chan struct{}), done: make(chan struct{})}
			releaseSource := sync.OnceFunc(func() { close(source.release) })
			returned := make(chan struct{})
			done := make(chan error, 1)
			t.Cleanup(func() {
				cancel()
				close(startRelease)
				releaseSource()
				for _, ch := range []<-chan struct{}{source.done, returned} {
					select {
					case <-ch:
					case <-time.After(5 * time.Second):
						t.Error("copy goroutine did not finish during cleanup")
						return
					}
				}
				_ = writer.Close()
			})
			go func() {
				defer close(returned)
				_, err := writer.ReadFrom(source)
				done <- err
			}()
			select {
			case <-startEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("ReadFrom did not enter start_large_file")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("ReadFrom error = %v, want context.Canceled", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ReadFrom waited for start_large_file after cancellation")
			}
			releaseSource()
			select {
			case <-source.done:
			case <-time.After(5 * time.Second):
				t.Fatal("copy goroutine did not finish after releasing the source")
			}
			if source.n != 0 || !errors.Is(source.err, errCopyAbandoned) {
				t.Errorf("late Write = (%d, %v), want (0, b2: copy abandoned)", source.n, source.err)
			}
		})
	}
}

func TestWriterReadFromCancellationDoesNotWaitForCleanup(t *testing.T) {
	for _, blocked := range []string{"cancel_large_file", "errf"} {
		for _, failure := range []string{"sendChunk", "upload_part", "getErr"} {
			t.Run(blocked+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				partEntered := make(chan struct{})
				enterPart := sync.OnceFunc(func() { close(partEntered) })
				cancelEntered := make(chan struct{})
				callbackEntered := make(chan struct{})
				partRelease := make(chan struct{})
				cancelRelease := make(chan struct{})
				callbackRelease := make(chan struct{})
				bufferEntered := make(chan struct{})
				bufferRelease := make(chan struct{})
				releasePart := sync.OnceFunc(func() { close(partRelease) })
				releaseCancel := sync.OnceFunc(func() { close(cancelRelease) })
				releaseCallback := sync.OnceFunc(func() { close(callbackRelease) })
				releaseBuffer := sync.OnceFunc(func() { close(bufferRelease) })
				if blocked != "cancel_large_file" {
					releaseCancel()
				}
				if blocked != "errf" {
					releaseCallback()
				}
				await := func(ch <-chan struct{}, what string) {
					t.Helper()
					select {
					case <-ch:
					case <-time.After(5 * time.Second):
						t.Fatalf("timed out waiting for %s", what)
					}
				}

				srv := b2wiretest.New(t)
				srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
				srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
				srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
				srv.Reply("upload_part", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"part failed"}`)
				srv.Handle("b2_cancel_large_file", func(r b2wiretest.Request) b2wiretest.Response {
					b2wiretest.AssertJSON(t, r, "fileId", "large-id")
					close(cancelEntered)
					<-cancelRelease
					return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"cleanup failed"}`}
				})
				transport := bucketInfoRoundTripper(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/upload_part" {
						enterPart()
						// Hold the worker so cancellation makes the producer's sendChunk fail.
						<-partRelease
					}
					return http.DefaultTransport.RoundTrip(r)
				})
				client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()), Transport(transport))
				if err != nil {
					t.Fatal(err)
				}
				bucket, err := client.NewBucket(ctx, "copy-cleanup", &BucketAttrs{Type: Private})
				if err != nil {
					t.Fatal(err)
				}
				callbackResult := make(chan error, 1)
				writer := bucket.Object("cancelled").NewWriter(ctx, WithCancelOnError(context.Background, func(err error) {
					callbackResult <- err
					close(callbackEntered)
					<-callbackRelease
				}))
				// The recording server does not enforce B2's minimum part size.
				writer.ChunkSize = 3
				if failure == "getErr" {
					buffers := 0
					writer.newBuffer = func() (writeBuffer, error) {
						buffers++
						if buffers == 2 {
							// The producer still owns the copy guard after handing off part 1.
							close(bufferEntered)
							<-bufferRelease
						}
						return newMemoryBuffer(), nil
					}
				}
				source, releaseSource := newBlockedCopyReader(t)
				releaseSource()
				returned := make(chan struct{})
				done := make(chan error, 1)
				t.Cleanup(func() {
					releaseCancel()
					releaseCallback()
					releasePart()
					releaseBuffer()
					await(source.done, "copy goroutine cleanup")
					await(returned, "ReadFrom goroutine cleanup")
					workersDone := make(chan struct{})
					go func() { writer.wg.Wait(); close(workersDone) }()
					await(workersDone, "upload worker cleanup")
					_ = writer.Close()
				})
				go func() {
					defer close(returned)
					_, err := writer.ReadFrom(source)
					done <- err
				}()
				await(partEntered, "first part upload")
				if failure == "getErr" {
					await(bufferEntered, "producer buffer allocation")
				}
				if failure != "sendChunk" {
					releasePart()
				} else {
					cancel()
				}
				entered := cancelEntered
				if blocked == "errf" {
					entered = callbackEntered
				}
				await(entered, "blocked "+blocked)
				cancel()
				releaseBuffer()
				select {
				case err := <-done:
					// A worker's permanent error may win the race with ctx.Done.
					partFailed := failure != "sendChunk" && err != nil && strings.Contains(err.Error(), "b2_upload_part: 400: part failed")
					if !errors.Is(err, context.Canceled) && !partFailed {
						t.Fatalf("ReadFrom error = %v, want context.Canceled or the injected part failure", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("ReadFrom waited for blocked %s after cancellation", blocked)
				}
				releaseCancel()
				releaseCallback()
				await(callbackEntered, "cancel callback")
				if err := <-callbackResult; err == nil || !strings.Contains(err.Error(), "cleanup failed") {
					t.Fatalf("errf result = %v, want cancel_large_file's cleanup failed error", err)
				}
			})
		}
	}
}

func TestWriterCloseWaitsForErrorCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelEntered := make(chan struct{})
	cancelRelease := make(chan struct{})
	releaseCancel := sync.OnceFunc(func() { close(cancelRelease) })
	callbackEntered := make(chan struct{})
	callbackRelease := make(chan struct{})
	releaseCallback := sync.OnceFunc(func() { close(callbackRelease) })
	callbackDone := make(chan struct{})
	callbackResult := make(chan error, 1)
	srv := b2wiretest.New(t)
	srv.Reply("b2_list_buckets", http.StatusOK, `{"buckets":[]}`)
	srv.Reply("b2_create_bucket", http.StatusOK, noReplicationBucketJSON)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"large-id"}`)
	srv.Reply("upload_part", http.StatusBadRequest, `{"status":400,"code":"bad_request","message":"part failed"}`)
	srv.Handle("b2_cancel_large_file", func(r b2wiretest.Request) b2wiretest.Response {
		b2wiretest.AssertJSON(t, r, "fileId", "large-id")
		close(cancelEntered)
		<-cancelRelease
		return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"cleanup failed"}`}
	})
	client, err := NewClient(ctx, "account", "key", APIBase(srv.URL()))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "copy-cleanup", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	writer := bucket.Object("cancelled").NewWriter(ctx, WithCancelOnError(context.Background, func(err error) {
		callbackResult <- err
		close(callbackEntered)
		<-callbackRelease // Test-controlled gate; never waits on Close.
		close(callbackDone)
	}))
	// The recording server does not enforce B2's minimum part size.
	writer.ChunkSize = 3
	t.Cleanup(func() {
		releaseCancel()
		releaseCallback()
		workersDone := make(chan struct{})
		go func() { writer.wg.Wait(); close(workersDone) }()
		select {
		case <-workersDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for upload worker cleanup")
		}
		_ = writer.Close()
	})
	// The worker fails while the producer is handing off the next full chunk.
	if _, err := writer.Write([]byte("payload")); err == nil || !strings.Contains(err.Error(), "part failed") {
		t.Fatalf("Write error = %v, want the worker's part failed error", err)
	}
	select {
	case <-cancelEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter cancel_large_file")
	}
	closed := make(chan error, 1)
	go func() {
		closed <- writer.Close()
	}()
	releaseCancel()
	select {
	case <-callbackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("errf did not start after cancel_large_file completed")
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned while errf was blocked: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	releaseCallback()
	select {
	case err := <-closed:
		select {
		case <-callbackDone:
		default:
			t.Error("errf had not completed when Close returned")
		}
		if err == nil || !strings.Contains(err.Error(), "part failed") {
			t.Errorf("Close error = %v, want the worker's part failed error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after worker cleanup completed")
	}
	if err := <-callbackResult; err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("errf result = %v, want cancel_large_file's cleanup failed error", err)
	}
}
