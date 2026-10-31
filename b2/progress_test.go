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
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type progressSample struct {
	complete int64
	total    int64
}

func TestProgressTerminalCallbackAfterAlreadyDelivered(t *testing.T) {
	first := make(chan struct{})
	var mu sync.Mutex
	var samples []progressSample
	p := newProgressReporter(func(complete, total int64) {
		mu.Lock()
		samples = append(samples, progressSample{complete, total})
		if len(samples) == 1 {
			close(first)
		}
		mu.Unlock()
	}, 5)
	p.add(5)
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("first progress sample was not delivered")
	}
	p.finish()
	got := progressSnapshot(&mu, &samples)
	if len(got) != 2 || got[0] != (progressSample{5, 5}) || got[1] != (progressSample{5, 5}) {
		t.Fatalf("progress samples = %+v, want update and guaranteed terminal", got)
	}
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

type progressHeadFailureBucket struct{ beBucketInterface }

func (b progressHeadFailureBucket) downloadFileByName(ctx context.Context, name string, offset, size int64, head bool, key *SSECKey) (beFileReaderInterface, error) {
	if head {
		return nil, errors.New("HEAD unavailable")
	}
	return b.beBucketInterface.downloadFileByName(ctx, name, offset, size, false, key)
}

func (b slowProgressBucket) downloadFileByName(ctx context.Context, name string, offset, size int64, head bool, key *SSECKey) (beFileReaderInterface, error) {
	reader, err := b.beBucketInterface.downloadFileByName(ctx, name, offset, size, head, key)
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
func (progressSkipFile) copyPart(context.Context, string, int, string) (beFilePartInterface, error) {
	return nil, nil
}

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

	writerEntered := make(chan struct{})
	writerAcknowledged := make(chan struct{})
	writerGate := make(chan struct{})
	releaseWriter := sync.OnceFunc(func() { close(writerGate) })
	defer releaseWriter()
	var writerOnce sync.Once
	var writer *Writer
	writer = bucket.Object("upload").NewWriter(ctx, WithProgress(func(_, _ int64) {
		writerOnce.Do(func() {
			close(writerEntered)
			<-writerGate
			_ = writer.status()
			close(writerAcknowledged)
		})
	}))
	if _, err := writer.ReadFrom(bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() { writeDone <- writer.Close() }()
	select {
	case <-writerEntered:
	case err := <-writeDone:
		t.Fatalf("upload completed without a progress callback: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("upload callback did not enter")
	}
	releaseWriter()
	select {
	case <-writerAcknowledged:
	case <-time.After(5 * time.Second):
		t.Fatal("upload callback did not return from writer.status")
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow reentrant upload callback deadlocked")
	}

	readerEntered := make(chan struct{})
	readerAcknowledged := make(chan struct{})
	readerGate := make(chan struct{})
	releaseReader := sync.OnceFunc(func() { close(readerGate) })
	defer releaseReader()
	var readerOnce sync.Once
	reader := bucket.Object("upload").NewRangeReader(ctx, 0, int64(len(payload)))
	reader.ChunkSize = len(payload) + 1
	reader.Progress = func(_, _ int64) {
		readerOnce.Do(func() {
			close(readerEntered)
			<-readerGate
			_ = reader.status()
			close(readerAcknowledged)
		})
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
	case <-readerEntered:
	case err := <-readDone:
		t.Fatalf("download completed without a progress callback: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("download callback did not enter")
	}
	releaseReader()
	select {
	case <-readerAcknowledged:
	case <-time.After(5 * time.Second):
		t.Fatal("download callback did not return from reader.status")
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow reentrant download callback deadlocked")
	}
}

func TestProgressCallbackCanReenterClientStatus(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-reenter-status", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	callbackDone := make(chan struct{}, 1)
	writer := bucket.Object("upload").NewWriter(ctx, WithProgress(func(_, _ int64) {
		_ = client.Status()
		select {
		case callbackDone <- struct{}{}:
		default:
		}
	}))
	if _, err := writer.Write([]byte("reentrant status")); err != nil {
		t.Fatal(err)
	}
	outerDone := make(chan error, 1)
	go func() { outerDone <- writer.Close() }()
	select {
	case err := <-outerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback re-entering Client.Status during upload deadlocked")
	}
	select {
	case <-callbackDone:
	default:
		t.Fatal("upload callback did not re-enter Client.Status")
	}

	root := client.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-reenter-status"]["download"] = string(bytes.Repeat([]byte("x"), 128_000))
	bucket.b = slowProgressBucket{bucket.b}
	reader := bucket.Object("download").NewReader(ctx)
	reader.ChunkSize = 256_000
	readerStatus := make(chan struct{}, 1)
	reader.Progress = func(_, _ int64) {
		_ = client.Status()
		select {
		case readerStatus <- struct{}{}:
		default:
		}
	}
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("callback re-entering Client.Status during download deadlocked")
	}
	select {
	case <-readerStatus:
	default:
		t.Fatal("download callback did not re-enter Client.Status")
	}
}

func TestProgressConcurrentCloseWaitsForFinalCallback(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-concurrent-close", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("progress"), 1024)

	t.Run("writer", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var mu sync.Mutex
		var samples []progressSample
		writer := bucket.Object("upload").NewWriter(ctx, WithProgress(func(complete, total int64) {
			mu.Lock()
			first := len(samples) == 0
			mu.Unlock()
			if first {
				close(started)
				<-release
			}
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}))
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		closed := make(chan error, 1)
		go func() { closed <- writer.Close() }()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("upload callback did not start")
		}
		select {
		case err := <-closed:
			close(release)
			t.Fatalf("Writer.Close returned before callback finished: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), -1, false)
	})

	t.Run("reader", func(t *testing.T) {
		root := client.backend.(*beRoot).b2i.(*testRoot)
		root.bucketMap["progress-concurrent-close"]["download"] = string(payload)
		bucket.b = slowProgressBucket{bucket.b}
		started := make(chan struct{})
		release := make(chan struct{})
		var mu sync.Mutex
		var samples []progressSample
		reader := bucket.Object("download").NewRangeReader(ctx, 0, int64(len(payload)))
		reader.Progress = func(complete, total int64) {
			mu.Lock()
			first := len(samples) == 0
			mu.Unlock()
			if first {
				close(started)
				<-release
			}
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}
		readDone := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, reader); close(readDone) }()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("download callback did not start")
		}
		// Let the download finish while its first callback remains blocked.
		// Close must still wait for the owner to deliver the terminal sample.
		reader.wg.Wait()
		closed := make(chan error, 1)
		go func() { closed <- reader.Close() }()
		select {
		case err := <-closed:
			close(release)
			t.Fatalf("Reader.Close returned before callback finished: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		<-readDone
		assertProgressSamples(t, progressSnapshot(&mu, &samples), int64(len(payload)), int64(len(payload)), false)
	})
}

func TestProgressRangeBeyondEOFUsesActualTotal(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-short-range", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	root := client.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-short-range"]["file"] = "hello world"
	reader := bucket.Object("file").NewRangeReader(ctx, 6, 10)
	var samples []progressSample
	reader.Progress = func(complete, total int64) {
		samples = append(samples, progressSample{complete, total})
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "world" {
		t.Fatalf("downloaded %q, want world", got)
	}
	assertProgressSamples(t, samples, 5, 5, false)
}

func TestProgressRangeUnknownWhenHEADUnavailable(t *testing.T) {
	ctx := context.Background()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-head-unavailable", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	root := client.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-head-unavailable"]["file"] = "hello world"
	bucket.b = progressHeadFailureBucket{bucket.b}
	reader := bucket.Object("file").NewRangeReader(ctx, 6, 10)
	var mu sync.Mutex
	var samples []progressSample
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
	if string(got) != "world" {
		t.Fatalf("downloaded %q, want world", got)
	}
	assertProgressSamples(t, progressSnapshot(&mu, &samples), 5, -1, false)
}

func TestProgressCancellationHasNoCallbackAfterTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(ctx, "progress-cancel", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	root := client.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-cancel"]["file"] = "x"
	reader := bucket.Object("file").NewReader(ctx)
	started := make(chan struct{})
	release := make(chan struct{})
	releaseCallback := sync.OnceFunc(func() { close(release) })
	defer releaseCallback()
	terminal := make(chan struct{})
	var mu sync.Mutex
	var samples []progressSample
	reader.Progress = func(complete, total int64) {
		mu.Lock()
		samples = append(samples, progressSample{complete, total})
		number := len(samples)
		mu.Unlock()
		switch number {
		case 1:
			close(started)
			<-release
		case 2:
			close(terminal)
		}
	}
	readDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(readDone) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not start before cancellation")
	}
	if got := progressSnapshot(&mu, &samples); len(got) != 1 || got[0] != (progressSample{1, -1}) {
		t.Fatalf("first progress = %+v, want one (1, -1) sample", got)
	}
	cancel()
	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()
	select {
	case err := <-closed:
		releaseCallback()
		t.Fatalf("Reader.Close returned before terminal callback on cancel: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	releaseCallback()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reader.Close did not finish after releasing the callback")
	}
	select {
	case <-terminal:
	default:
		t.Fatal("cancelled reader did not deliver a separate terminal (1, -1) callback")
	}
	select {
	case <-reader.reporter.done:
	default:
		t.Fatal("progress reporter did not finish before Reader.Close returned")
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled read did not finish")
	}
	before := progressSnapshot(&mu, &samples)
	if len(before) != 2 || before[0] != (progressSample{1, -1}) || before[1] != (progressSample{1, -1}) {
		t.Fatalf("progress at completion = %+v, want [(1, -1), (1, -1)]", before)
	}
	select {
	case <-reader.reporter.wake: // finish may leave a pre-terminal wake queued.
	default:
	}
	reader.reporter.add(1) // A late metered read must not produce another callback.
	select {
	case <-reader.reporter.wake:
		t.Fatal("late progress update queued after terminal callback")
	case <-time.After(100 * time.Millisecond):
		// Give an incorrectly queued update time to reach the progress owner.
	}
	after := progressSnapshot(&mu, &samples)
	if len(after) != len(before) {
		t.Fatalf("callback after terminal call: before=%+v after=%+v", before, after)
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
