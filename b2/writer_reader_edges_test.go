package b2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestReaderChunkSizeOneAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		chunk int
		want  []string
	}{
		{"one byte", 1, 1, []string{"bytes=0-0", "bytes=1-1", "bytes=2-2"}},
		{"zero uses default", 0, 10_000_000, []string{"bytes=0-2"}},
		{"negative uses default", -1, 10_000_000, []string{"bytes=0-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Handle("one-byte", func(req b2wiretest.Request) b2wiretest.Response {
				body := map[string]string{"bytes=0-0": "a", "bytes=1-1": "b", "bytes=2-2": "c", "bytes=0-2": "abc"}[req.Header.Get("Range")]
				return b2wiretest.Response{Status: http.StatusPartialContent, Body: body}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r := bucket.Object("one-byte").NewRangeReader(ctx, 0, 3)
			r.ChunkSize = tc.size
			defer func() { _ = r.Close() }()
			r.init.Do(r.initFunc)
			if r.csize != tc.chunk {
				t.Fatalf("chunk size = %d, want %d", r.csize, tc.chunk)
			}
			got, err := io.ReadAll(r)
			if err != nil || string(got) != "abc" {
				t.Fatalf("ReadAll = %q, %v; want abc, nil", got, err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			var ranges []string
			for _, req := range srv.Calls("one-byte") {
				ranges = append(ranges, req.Header.Get("Range"))
			}
			if !reflect.DeepEqual(ranges, tc.want) {
				t.Fatalf("Range requests = %q, want %q", ranges, tc.want)
			}
		})
	}
}

type edgeWriteBuffer struct {
	writeBuffer
	write func([]byte) (int, error)
}

func (b edgeWriteBuffer) Write(p []byte) (int, error) { return b.write(p) }

func TestWriterBufferWriteErrorIsSticky(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"within chunk", "ab"},
		{"crosses chunk", "abcd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newProgressReviewBucket(t).Object("sticky").NewWriter(context.Background())
			w.ChunkSize = 3
			defer func() { _ = w.Close() }()
			want := errors.New("buffer write failed")
			calls := 0
			w.newBuffer = func() (writeBuffer, error) {
				buffer := newMemoryBuffer()
				return edgeWriteBuffer{buffer, func(p []byte) (int, error) {
					calls++
					if calls == 1 {
						return 0, want
					}
					return buffer.Write(p)
				}}, nil
			}
			if n, err := w.Write([]byte(tc.payload)); n != 0 || err != want {
				t.Fatalf("first Write = %d, %v; want 0, buffer write failed", n, err)
			}
			if n, err := w.Write([]byte("x")); n != 0 || err != want {
				t.Fatalf("second Write = %d, %v; want 0, original buffer error", n, err)
			}
			if calls != 1 {
				t.Fatalf("buffer Write calls = %d, want 1", calls)
			}
		})
	}
}

func TestWriterStatusIncludesAllThreeConcurrentParts(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"status-large"}`)
	srv.Reply("b2_finish_large_file", http.StatusOK, `{"fileId":"status-large","fileName":"status","action":"upload"}`)
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	releaseParts := sync.OnceFunc(func() { close(release) })
	srv.Handle("upload_part", func(b2wiretest.Request) b2wiretest.Response {
		entered <- struct{}{}
		<-release
		return b2wiretest.Response{Body: `{"contentSha1":"part-sha1"}`}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := bucket.Object("status").NewWriter(ctx)
	w.ChunkSize = 3
	w.ConcurrentUploads = 3
	defer func() { _ = w.Close() }()
	defer releaseParts()
	if n, err := w.ReadFrom(bytes.NewReader([]byte("abcdefghi"))); n != 9 || err != nil {
		t.Fatalf("ReadFrom = %d, %v; want 9, nil", n, err)
	}
	for range 3 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("three concurrent parts did not enter upload")
		}
	}
	if got := w.status().Progress; !reflect.DeepEqual(got, []float64{1, 1, 1}) {
		t.Fatalf("in-flight Progress = %v, want [1 1 1]", got)
	}
	// Closing release lets all three uploads complete before the final snapshot.
	releaseParts()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := w.status().Progress; !reflect.DeepEqual(got, []float64{1, 1, 1}) {
		t.Fatalf("final Progress = %v, want [1 1 1]", got)
	}
}

// Err snapshots the context before pausing the producer at its second handoff.
// Cancellation while paused must be observed by sendChunk's select.
type edgeHandoffContext struct {
	context.Context
	armed            atomic.Bool
	checked, release chan struct{}
}

func (c *edgeHandoffContext) Err() error {
	err := c.Context.Err()
	if err == nil && c.armed.CompareAndSwap(true, false) {
		close(c.checked)
		<-c.release
	}
	return err
}

func TestWriterDirectWriteUnblocksOnPermanentPartFailure(t *testing.T) {
	srv, bucket := newSHA1WireBucket(t)
	srv.Reply("b2_start_large_file", http.StatusOK, `{"fileId":"failed-large"}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := bucket.Object("direct-failure").NewWriter(ctx)
	w.ChunkSize = 3
	w.ConcurrentUploads = 1
	gate := &edgeHandoffContext{Context: w.ctx, checked: make(chan struct{}), release: make(chan struct{})}
	releaseGate := sync.OnceFunc(func() { close(gate.release) })
	w.ctx = gate
	partEntered := make(chan struct{})
	srv.Handle("upload_part", func(b2wiretest.Request) b2wiretest.Response {
		close(partEntered)
		<-gate.checked
		return b2wiretest.Response{Status: http.StatusBadRequest, Body: `{"status":400,"code":"bad_request","message":"permanent part failure"}`}
	})
	buffers := 0
	w.newBuffer = func() (writeBuffer, error) {
		buffers++
		buffer := newMemoryBuffer()
		if buffers == 2 {
			return edgeWriteBuffer{buffer, func(p []byte) (int, error) {
				n, err := buffer.Write(p)
				<-partEntered
				gate.armed.Store(true)
				return n, err
			}}, nil
		}
		return buffer, nil
	}
	done := make(chan struct{})
	var n int
	var writeErr error
	go func() { n, writeErr = w.Write([]byte("abcdefghi")); close(done) }()
	defer func() {
		cancel()
		releaseGate()
		if w.cdone != nil {
			close(w.cdone) // also releases the deliberately broken sendChunk select
		}
		<-done
		_ = w.Close()
	}()
	select {
	case <-gate.checked:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not reach its second chunk handoff")
	}
	select {
	case <-w.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("permanent part failure did not cancel writer")
	}
	releaseGate()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("direct Write blocked after permanent part failure")
	}
	code, reason := base.Code(writeErr)
	if n != 6 || code != 400 || reason != "permanent part failure" {
		t.Fatalf("Write = %d, %v (status %d, message %q); want 6, 400 permanent part failure", n, writeErr, code, reason)
	}
}
