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
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type blockedMeterSource struct{ entered, release chan struct{} }

func (s *blockedMeterSource) Read([]byte) (int, error) {
	close(s.entered)
	<-s.release
	return 0, io.EOF
}
func (s *blockedMeterSource) Reset() error { return nil }

func TestProgressStatusDoesNotWaitForBlockedRead(t *testing.T) {
	source := &blockedMeterSource{make(chan struct{}), make(chan struct{})}
	meter := &meteredReader{r: source, size: 1, progressSize: 1}
	client := &Client{sWriters: map[string]*Writer{"blocked": {smap: map[int]*meteredReader{1: meter}}}}
	readDone := make(chan struct{})
	go func() { _, _ = meter.Read(make([]byte, 1)); close(readDone) }()
	<-source.entered
	statusDone := make(chan struct{})
	go func() { client.Status(); close(statusDone) }()
	select {
	case <-statusDone:
	case <-time.After(2 * time.Second):
		close(source.release)
		<-readDone
		t.Fatal("Client.Status blocked on transfer I/O")
	}
	close(source.release)
	<-readDone
}

func newProgressReviewBucket(t *testing.T) *Bucket {
	t.Helper()
	client := &Client{backend: &beRoot{b2i: &testRoot{bucketMap: make(map[string]map[string]string), errs: &errCont{}}}}
	bucket, err := client.NewBucket(context.Background(), "progress-review", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func TestProgressZeroLengthVerifyDoesNotPanic(t *testing.T) {
	r := newProgressReviewBucket(t).Object("empty").NewRangeReader(context.Background(), 0, 0)
	var b [1]byte
	if n, err := r.Read(b[:]); n != 0 || err != io.EOF {
		t.Fatalf("Read = %d, %v", n, err)
	}
	if err, ok := r.Verify(); err != nil || ok {
		t.Fatalf("Verify = %v, %t; want nil, false", err, ok)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProgressCompletedReaderStatus(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	ctx := context.Background()
	object := bucket.Object("completed-status")
	writer := object.NewWriter(ctx)
	if n, err := io.WriteString(writer, "abcd"); n != 4 || err != nil {
		t.Fatalf("Write = %d, %v; want 4, nil", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		length   int64
		body     string
		progress []float64
	}{
		{"two chunks", 4, "abcd", []float64{1, 1}},
		{"zero length", 0, "", []float64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := object.NewRangeReader(ctx, 0, tc.length)
			reader.ChunkSize = 2
			reader.ConcurrentDownloads = 1
			defer func() { _ = reader.Close() }()
			if got, err := io.ReadAll(reader); string(got) != tc.body || err != nil {
				t.Fatalf("ReadAll = %q, %v; want %q, nil", got, err, tc.body)
			}
			// EOF leaves the reader registered until Close.
			status := bucket.c.Status()
			if len(status.Readers) != 1 {
				t.Fatalf("Client.Status readers = %d, want 1 before Close", len(status.Readers))
			}
			for _, current := range status.Readers {
				if !reflect.DeepEqual(current.Progress, tc.progress) {
					t.Fatalf("completed reader progress = %v, want %v", current.Progress, tc.progress)
				}
			}
		})
	}
}

func TestProgressResumePipeWithoutCallback(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := pr.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := pw.Write([]byte("pipe payload")); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	object := newProgressReviewBucket(t).Object("pipe")
	w := object.NewWriter(context.Background())
	w.Resume = true
	n, err := w.ReadFrom(pr)
	if err != nil || n != int64(len("pipe payload")) {
		t.Fatalf("ReadFrom = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := object.NewReader(context.Background())
	defer func() { _ = r.Close() }()
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "pipe payload" {
		t.Fatalf("uploaded content = %q, %v; want pipe payload, nil", got, err)
	}
}

func TestProgressResumePastEOFReportsZero(t *testing.T) {
	input := bytes.NewReader([]byte("x"))
	if _, err := input.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var samples []progressSample
	w := newProgressReviewBucket(t).Object("past-eof").NewWriter(context.Background(), WithProgress(func(n, total int64) { mu.Lock(); samples = append(samples, progressSample{n, total}); mu.Unlock() }))
	w.Resume = true
	if n, err := w.ReadFrom(input); n != 0 || err != nil {
		t.Fatalf("ReadFrom = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := progressSnapshot(&mu, &samples)
	if len(got) == 0 || got[len(got)-1] != (progressSample{0, 0}) {
		t.Fatalf("progress = %+v, want terminal 0/0", got)
	}
}

type blockingHeadBucket struct {
	beBucketInterface
	entered, release chan struct{}
}

func (b blockingHeadBucket) downloadFileByName(ctx context.Context, name string, offset, size int64, head bool, key *SSECKey) (beFileReaderInterface, error) {
	if head {
		close(b.entered)
		<-b.release
	}
	return b.beBucketInterface.downloadFileByName(ctx, name, offset, size, head, key)
}
func TestProgressCloseWaitsForInitialization(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	root := bucket.c.backend.(*beRoot).b2i.(*testRoot)
	root.bucketMap["progress-review"]["file"] = "payload"
	gate := blockingHeadBucket{bucket.b, make(chan struct{}), make(chan struct{})}
	bucket.b = gate
	r := bucket.Object("file").NewRangeReader(context.Background(), 0, 7)
	var mu sync.Mutex
	var samples []progressSample
	r.Progress = func(complete, total int64) {
		mu.Lock()
		samples = append(samples, progressSample{complete, total})
		mu.Unlock()
	}
	readDone := make(chan struct{})
	go func() { _, _ = io.ReadAll(r); close(readDone) }()
	<-gate.entered
	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	go func() { close(closeStarted); _ = r.Close(); close(closeDone) }()
	<-closeStarted
	select {
	case <-closeDone:
		close(gate.release)
		t.Fatal("Close returned before blocked initialization finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(gate.release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after initialization")
	}
	<-readDone
	got := progressSnapshot(&mu, &samples)
	if len(got) == 0 || got[len(got)-1] != (progressSample{0, -1}) {
		t.Fatalf("progress after Close = %+v, want final 0/-1 after cancelled initialization", got)
	}
}

type pipeProgressBody struct {
	r               *io.PipeReader
	entered, exited chan struct{}
	once            sync.Once
}

func (b *pipeProgressBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	n, err := b.r.Read(p)
	close(b.exited)
	return n, err
}
func (b *pipeProgressBody) Close() error                                    { return b.r.Close() }
func (b *pipeProgressBody) stats() (int, string, string, map[string]string) { return 1, "", "", nil }
func (b *pipeProgressBody) id() string                                      { return "pipe" }

type pipeProgressBucket struct {
	beBucketInterface
	body *pipeProgressBody
}

func (b pipeProgressBucket) downloadFileByName(context.Context, string, int64, int64, bool, *SSECKey) (beFileReaderInterface, error) {
	return b.body, nil
}
func TestProgressCancelClosesBlockedBody(t *testing.T) {
	bucket := newProgressReviewBucket(t)
	pr, pw := io.Pipe()
	defer func() {
		if err := pw.Close(); err != nil {
			t.Error(err)
		}
	}()
	body := &pipeProgressBody{r: pr, entered: make(chan struct{}), exited: make(chan struct{})}
	bucket.b = pipeProgressBucket{bucket.b, body}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := bucket.Object("pipe").NewReader(ctx)
	r.Progress = func(int64, int64) {}
	readDone := make(chan struct{})
	go func() { _, _ = io.ReadAll(r); close(readDone) }()
	<-body.entered
	cancel()
	closeDone := make(chan struct{})
	go func() { _ = r.Close(); close(closeDone) }()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked on blocked response Read")
	}
	select {
	case <-body.exited:
	case <-time.After(time.Second):
		t.Fatal("response Read goroutine leaked")
	}
	<-readDone
}
