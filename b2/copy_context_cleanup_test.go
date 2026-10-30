package b2

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

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
		close(callbackDone) // No wait on Close or on any other channel.
	}))
	// The recording server does not enforce B2's minimum part size.
	writer.ChunkSize = 3
	t.Cleanup(func() {
		releaseCancel()
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
	type closeResult struct {
		err               error
		callbackCompleted bool
	}
	closeStarted := make(chan struct{})
	closed := make(chan closeResult, 1)
	go func() {
		close(closeStarted)
		err := writer.Close()
		completed := false
		select {
		case <-callbackDone:
			completed = true
		default:
		}
		closed <- closeResult{err: err, callbackCompleted: completed}
	}()
	<-closeStarted
	select {
	case got := <-closed:
		t.Fatalf("errf completed when Close returned = %t, want true", got.callbackCompleted)
	case <-time.After(time.Second):
		// Release cleanup independently of Close, so even a broken Close cannot deadlock the test.
	}
	releaseCancel()
	select {
	case got := <-closed:
		if !got.callbackCompleted {
			t.Error("errf completed when Close returned = false, want true")
		}
		if got.err == nil || !strings.Contains(got.err.Error(), "part failed") {
			t.Errorf("Close error = %v, want the worker's part failed error", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after worker cleanup completed")
	}
	if err := <-callbackResult; err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("errf result = %v, want cancel_large_file's cleanup failed error", err)
	}
}
