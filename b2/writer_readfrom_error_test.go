package b2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

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
			writer.ChunkSize = 3
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
