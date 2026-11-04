package b2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

const completionABCSHA1 = "a9993e364706816aba3e25717850c26c9cd0d89d"

func completionResult(t *testing.T, timeout time.Duration, operation func(context.Context) error) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- operation(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		cancel()
		// Join before failing so fixture cleanup cannot race a running operation.
		err := <-done
		t.Fatalf("operation did not complete within %s; after cancellation: %v", timeout, err)
		return nil
	}
}

func inCompletionProcess(t *testing.T) bool {
	t.Helper()
	const child = "BLAZER_COMPLETION_CHILD"
	if os.Getenv(child) == t.Name() {
		return true
	}
	// Mutated reader/writer launch loops can ignore cancellation or block on
	// internal channels. A process boundary lets the parent stop and join them.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Allow slow race-enabled runners ample startup time. CommandContext kills
	// an unbounded launch loop, and completionResult waits for the child to exit.
	if err := completionResult(t, time.Minute, func(ctx context.Context) error {
		command := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.timeout=2m")
		command.Env = append(os.Environ(), child+"="+t.Name())
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("isolated completion: %w\n%s", err, output)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestWriterWorkerStartupCompletes(t *testing.T) {
	if !inCompletionProcess(t) {
		return
	}
	writer := newProgressReviewBucket(t).Object("startup").NewWriter(context.Background())
	writer.ChunkSize = 3 // Deliberately below B2's 5MB minimum to exercise multipart with the test fake.
	writer.ConcurrentUploads = 1
	if n, err := io.WriteString(writer, "abcdefghi"); n != 9 || err != nil {
		t.Fatalf("Write = %d, %v; want 9, nil", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTransferOperationsComplete(t *testing.T) {
	if !inCompletionProcess(t) {
		return
	}
	for _, seekable := range []bool{false, true} {
		t.Run(fmt.Sprintf("seekable=%t", seekable), func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"completion-id"}`)
			srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"completion-id","fileName":"completion","action":"upload"}`)
			srv.Handle("completion", func(request b2wiretest.Request) b2wiretest.Response {
				responses := map[string]b2wiretest.Response{
					"bytes=0-2": {Status: http.StatusPartialContent, Body: "abc", Header: http.Header{"Content-Range": {"bytes 0-2/9"}}},
					"bytes=3-5": {Status: http.StatusPartialContent, Body: "def", Header: http.Header{"Content-Range": {"bytes 3-5/9"}}},
					"bytes=6-8": {Status: http.StatusPartialContent, Body: "ghi", Header: http.Header{"Content-Range": {"bytes 6-8/9"}}},
				}
				return responses[request.Header.Get("Range")]
			})
			if err := completionResult(t, 5*time.Second, func(ctx context.Context) error {
				object := bucket.Object("completion")
				writer := object.NewWriter(ctx)
				defer func() { _ = writer.Close() }()
				writer.ChunkSize = 3         // Deliberately below B2's 5MB minimum to exercise multipart with the recording fake.
				writer.ConcurrentUploads = 0 // Must still start one worker.
				const payload = "abcdefghi"
				var n int64
				var err error
				if seekable {
					// Omit ReaderAt so ReadFrom also exercises enReaderAt.
					n, err = writer.ReadFrom(struct{ io.ReadSeeker }{bytes.NewReader([]byte(payload))})
				} else {
					var written int
					written, err = io.WriteString(writer, payload)
					n = int64(written)
				}
				if err != nil || n != 9 {
					return fmt.Errorf("upload = %d, %v; want 9, nil", n, err)
				}
				if err := writer.Close(); err != nil {
					return err
				}
				reader := object.NewRangeReader(ctx, 0, 9)
				reader.ChunkSize = 3
				reader.ConcurrentDownloads = 1
				got, err := io.ReadAll(reader)
				closeErr := reader.Close()
				if err != nil || closeErr != nil || string(got) != payload {
					return fmt.Errorf("download = %q, %v; Close = %v; want %q, nil, nil", got, err, closeErr, payload)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			parts := srv.Calls("upload_part")
			if len(parts) != 3 {
				t.Fatalf("upload requests = %d, want 3", len(parts))
			}
			wantParts := map[string]string{"1": "abc", "2": "def", "3": "ghi"}
			for _, part := range parts {
				body := part.Body
				if part.Header.Get("X-Bz-Content-Sha1") == "hex_digits_at_end" && len(body) >= 40 {
					body = body[:len(body)-40]
				}
				number := part.Header.Get("X-Bz-Part-Number")
				if want, ok := wantParts[number]; !ok || string(body) != want {
					t.Fatalf("upload part %s = %q, want %q", number, body, want)
				}
				delete(wantParts, number)
			}
		})
	}
}

func TestZeroLengthProgressRangeDoesNotHead(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("missing", http.StatusOK, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader := bucket.Object("missing").NewRangeReader(ctx, 0, 0)
	reader.Progress = func(int64, int64) {}
	if n, err := reader.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatalf("Read = %d, %v; want 0, EOF", n, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(srv.Calls("missing")); got != 0 {
		t.Errorf("download requests = %d, want 0", got)
	}
}

type startupErrorBucket struct {
	beBucketInterface
	err error
}

func (b startupErrorBucket) startLargeFile(context.Context, string, string, map[string]string, *FileRetention, LegalHold, *uploadEncryption) (beLargeFileInterface, error) {
	return nil, b.err
}

func TestSendChunkReturnsStartupError(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	want := errors.New("start large file failed")
	bucket.b = startupErrorBucket{bucket.b, want}
	err := completionResult(t, 5*time.Second, func(ctx context.Context) error {
		writer := bucket.Object("startup").NewWriter(ctx)
		return writer.sendChunk()
	})
	if !errors.Is(err, want) {
		t.Fatalf("sendChunk = %v, want startup error", err)
	}
}

func TestReaderAtReturnsRequestedBytesAndPreservesPosition(t *testing.T) {
	source := bytes.NewReader([]byte("abcdef"))
	if _, err := source.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var got [2]byte
	if n, err := enReaderAt(source).ReadAt(got[:], 1); n != 2 || err != nil || string(got[:]) != "bc" {
		t.Fatalf("ReadAt = %d, %v, %q; want 2, nil, bc", n, err, got)
	}
	if position, err := source.Seek(0, io.SeekCurrent); position != 4 || err != nil {
		t.Fatalf("position = %d, %v; want 4, nil", position, err)
	}
}

func TestComposeContextReaderReturnsBytes(t *testing.T) {
	reader := composeContextReader{ctx: context.Background(), reader: bytes.NewBufferString("abc")}
	var got [3]byte
	if n, err := reader.Read(got[:]); n != 3 || err != nil || string(got[:]) != "abc" {
		t.Fatalf("Read = %d, %v, %q; want 3, nil, abc", n, err, got)
	}
}

func TestTerminalListingPageReturnsEOF(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	for _, list := range []lister{bucket.listCurrentObjects, bucket.listObjects} {
		objects, next, err := list(context.Background(), 100, nil)
		if len(objects) != 0 || next != nil || err != io.EOF {
			t.Fatalf("terminal page = %v, %v, %v; want no objects, nil cursor, EOF", objects, next, err)
		}
	}
}

func TestResumeStopsAtFinalPartPage(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_list_unfinished_large_files", http.StatusOK, `{"files":[{"fileId":"resume-id","fileName":"resume","action":"start"}],"nextFileId":null}`)
	srv.ReplySequence("b2_list_parts",
		b2wiretest.Response{Body: fmt.Sprintf(`{"parts":[{"partNumber":1,"contentLength":3,"contentSha1":%q}],"nextPartNumber":null}`, completionABCSHA1)},
		b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"unexpected extra part page"}`})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	writer := bucket.Object("resume").NewWriter(ctx)
	writer.Resume = true
	large, err := writer.getLargeFile()
	if err != nil || large == nil {
		t.Fatalf("getLargeFile = %v, %v; want resumed file, nil", large, err)
	}
	if got := len(srv.Calls("b2_list_parts")); got != 1 {
		t.Errorf("list parts requests = %d, want 1", got)
	}
	if len(writer.seen) != 1 || writer.seen[1] != completionABCSHA1 {
		t.Errorf("resumed hashes = %v, want part 1 abc SHA-1", writer.seen)
	}
}
