// Copyright 2026, the Blazer authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package b2

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type progressSample struct {
	complete int64
	total    int64
}

func TestProgressCallbacks(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{
		bucketMap: make(map[string]map[string]string),
		errs:      &errCont{},
	}}}
	root := client.backend.(*beRoot).b2i.(*testRoot)
	bucket, err := client.NewBucket(ctx, "progress-test", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("progress callback payload"), 6000)

	t.Run("upload", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		writer := bucket.Object("upload").NewWriter(ctx, WithProgress(func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}))
		if _, err := writer.ReadFrom(&slowReadSeeker{Reader: bytes.NewReader(payload)}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), int64(len(payload)), true)
	})

	t.Run("unknown upload total", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		writer := bucket.Object("unknown-upload").NewWriter(ctx, WithProgress(func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}))
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), -1, false)
	})

	t.Run("resume with seeker has known total", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		writer := bucket.Object("resume-upload").NewWriter(ctx, WithProgress(func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}))
		writer.Resume = true
		seeker := bytes.NewReader(payload)
		if _, err := seeker.Seek(7, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.ReadFrom(seeker); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		want := int64(len(payload) - 7)
		assertProgressSamples(t, progressSnapshot(&mu, &samples), want, want, false)
	})

	t.Run("ranged download", func(t *testing.T) {
		root.bucketMap["progress-test"]["download"] = string(payload)
		bucket.b = slowProgressBucket{bucket.b}
		var mu sync.Mutex
		var samples []progressSample
		reader := bucket.Object("download").NewRangeReader(ctx, 0, int64(len(payload)))
		reader.ChunkSize = len(payload) + 1 // one chunk, many metered Reads
		reader.Progress = func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}
		got, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("downloaded %q, want %q", got, payload)
		}

		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), int64(len(payload)), true)
	})

	t.Run("parallel ranged download stays within total", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		length := int64(len(payload) / 2)
		reader := bucket.Object("download").NewRangeReader(ctx, 0, length)
		reader.ChunkSize = len(payload) / 8
		reader.ConcurrentDownloads = 3
		reader.Progress = func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}
		got, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload[:length]) {
			t.Fatalf("ranged bytes differ")
		}
		assertProgressSamples(t, progressSnapshot(&mu, &samples), length, length, true)
	})

	t.Run("full download has unknown total", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		reader := bucket.Object("download").NewReader(ctx)
		reader.ChunkSize = len(payload) + 1
		reader.Progress = func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			t.Fatal(err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), -1, true)
	})

	t.Run("zero-length range has known zero total", func(t *testing.T) {
		var samples []progressSample
		reader := bucket.Object("download").NewRangeReader(ctx, 0, 0)
		reader.Progress = func(complete, total int64) {
			samples = append(samples, progressSample{complete, total})
		}
		var b [1]byte
		if n, err := reader.Read(b[:]); n != 0 || err != io.EOF {
			t.Fatalf("Read = (%d, %v), want (0, EOF)", n, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
		assertProgressSamples(t, samples, 0, 0, false)
	})
}

func TestProgressMeterResetDoesNotDoubleCount(t *testing.T) {
	payload := []byte("payload")
	buffer := newNonBuffer(bytes.NewReader(payload), 0, int64(len(payload)))
	stream, err := buffer.Reader()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var samples []progressSample
	reporter := newProgressReporter(func(complete, total int64) {
		mu.Lock()
		samples = append(samples, progressSample{complete, total})
		mu.Unlock()
	}, int64(len(payload)))
	meter := newMeteredReader(stream, buffer, reporter)
	if _, err := io.Copy(io.Discard, meter); err != nil {
		t.Fatal(err)
	}
	if err := meter.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, meter); err != nil {
		t.Fatal(err)
	}
	reporter.finish()
	assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), int64(len(payload)), false)
}

type slowReadSeeker struct{ *bytes.Reader }

func (r *slowReadSeeker) ReadAt(p []byte, off int64) (int, error) {
	if len(p) > 4096 {
		p = p[:4096]
	}
	n, err := r.Reader.ReadAt(p, off)
	time.Sleep(time.Millisecond)
	return n, err
}

type slowProgressBucket struct{ beBucketInterface }

func (b slowProgressBucket) downloadFileByName(ctx context.Context, name string, offset, size int64, head bool) (beFileReaderInterface, error) {
	reader, err := b.beBucketInterface.downloadFileByName(ctx, name, offset, size, head)
	if err != nil {
		return nil, err
	}
	return slowProgressFileReader{reader}, nil
}

type slowProgressFileReader struct{ beFileReaderInterface }

func (r slowProgressFileReader) Read(p []byte) (int, error) {
	if len(p) > 4096 {
		p = p[:4096]
	}
	n, err := r.beFileReaderInterface.Read(p)
	time.Sleep(time.Millisecond)
	return n, err
}

type progressSkipFile struct{}

func (progressSkipFile) getUploadPartURL(context.Context) (beFileChunkInterface, error) {
	return progressSkipChunk{}, nil
}
func (progressSkipFile) finishLargeFile(context.Context) (beFileInterface, error) { return nil, nil }
func (progressSkipFile) cancel(context.Context) error                             { return nil }

type progressSkipChunk struct{}

func (progressSkipChunk) reload(context.Context) error { return nil }
func (progressSkipChunk) uploadPart(context.Context, readResetter, string, int, int) (int, error) {
	return 0, nil
}

func TestProgressResumeSkipDoesNotReadClosedBuffer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	buf := newMemoryBuffer()
	if _, err := buf.Write([]byte("already uploaded")); err != nil {
		t.Fatal(err)
	}
	notified := make(chan int64, 2)
	w := &Writer{
		ctx: ctx, cancel: cancel,
		file: progressSkipFile{},
		seen: map[int]string{1: buf.Hash()}, ready: make(chan chunk), cdone: make(chan struct{}),
		smap:     make(map[int]*meteredReader),
		reporter: newProgressReporter(func(complete, _ int64) { notified <- complete }, int64(buf.Len())),
	}
	w.thread()
	select {
	case w.ready <- chunk{id: 1, buf: buf}:
	case <-time.After(5 * time.Second):
		t.Fatal("resumed chunk was not accepted")
	}
	select {
	case got := <-notified:
		if got != int64(len("already uploaded")) {
			t.Fatalf("resumed progress = %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resumed chunk was not processed")
	}
	close(w.cdone)
	w.wg.Wait()
	w.reporter.finish()
}

func TestProgressSlowCallbackCanReenterTransfer(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-reentry", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("reentry"), 20_000)

	var writer *Writer
	writer = bucket.Object("upload").NewWriter(ctx, WithProgress(func(_, _ int64) {
		time.Sleep(time.Millisecond)
		_ = writer.status()
	}))
	if _, err := writer.ReadFrom(&slowReadSeeker{Reader: bytes.NewReader(payload)}); err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() { writeDone <- writer.Close() }()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow reentrant upload callback deadlocked")
	}

	bucket.b = slowProgressBucket{bucket.b}
	reader := bucket.Object("upload").NewRangeReader(ctx, 0, int64(len(payload)))
	reader.ChunkSize = len(payload) + 1
	reader.Progress = func(_, _ int64) {
		time.Sleep(time.Millisecond)
		_ = reader.status()
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, reader)
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow reentrant download callback deadlocked")
	}
}

func TestProgressCallbackCanReenterClose(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-reenter-close", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	callbackDone := make(chan error, 1)
	var writer *Writer
	writer = bucket.Object("empty").NewWriter(ctx, WithProgress(func(_, _ int64) {
		callbackDone <- writer.Close()
	}))
	outerDone := make(chan error, 1)
	go func() { outerDone <- writer.Close() }()
	for _, done := range []<-chan error{callbackDone, outerDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("callback re-entering Writer.Close deadlocked")
		}
	}

	root := client.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-reenter-close"]["download"] = string(bytes.Repeat([]byte("x"), 128_000))
	bucket.b = slowProgressBucket{bucket.b}
	reader := bucket.Object("download").NewReader(ctx)
	reader.ChunkSize = 256_000
	readerClosed := make(chan error, 1)
	var once sync.Once
	reader.Progress = func(_, _ int64) {
		once.Do(func() { readerClosed <- reader.Close() })
	}
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader) // cancellation is expected
		close(readDone)
	}()
	select {
	case err := <-readerClosed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback re-entering Reader.Close deadlocked")
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not stop after callback Close")
	}
}

func progressSnapshot(mu *sync.Mutex, samples *[]progressSample) []progressSample {
	mu.Lock()
	defer mu.Unlock()
	return append([]progressSample(nil), (*samples)...)
}

func assertProgressSamples(t *testing.T, samples []progressSample, wantComplete, wantTotal int64, intermediate bool) {
	t.Helper()
	if len(samples) == 0 {
		t.Fatal("progress callback was not called")
	}
	var previous int64
	var sawIntermediate bool
	for i, sample := range samples {
		if sample.complete < previous {
			t.Fatalf("sample %d complete = %d, previous = %d", i, sample.complete, previous)
		}
		if sample.total != wantTotal {
			t.Fatalf("sample %d total = %d, want %d", i, sample.total, wantTotal)
		}
		previous = sample.complete
		if sample.complete > 0 && sample.complete < wantComplete {
			sawIntermediate = true
		}
	}
	if got := samples[len(samples)-1].complete; got != wantComplete {
		t.Fatalf("final complete = %d, want %d", got, wantComplete)
	}
	if intermediate && !sawIntermediate {
		t.Fatalf("no intermediate byte-level progress: %+v", samples)
	}
}
