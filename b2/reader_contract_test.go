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
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/Backblaze/blazer/x/b2sim"
)

// EOF remains terminal for subsequent reads while the caller's context is live.
// https://pkg.go.dev/io#Reader
func TestReaderReadAfterEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	object := newProgressReviewBucket(t).Object("eof")
	writer := object.NewWriter(ctx)
	if n, err := io.WriteString(writer, "abc"); n != 3 || err != nil {
		t.Fatalf("Write = (%d, %v), want (3, nil)", n, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	reader := object.NewReader(ctx)
	t.Cleanup(func() { _ = reader.Close() })
	if got, err := io.ReadAll(reader); string(got) != "abc" || err != nil {
		t.Fatalf("ReadAll = (%q, %v), want (abc, nil)", got, err)
	}
	var buf [1]byte
	for i := range 2 {
		if n, err := reader.Read(buf[:]); n != 0 || err != io.EOF {
			t.Errorf("Read after EOF #%d = (%d, %v), want (0, EOF)", i+1, n, err)
		}
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("caller context = %v, want nil", err)
	}
}

func TestReaderZeroLengthRangeMakesNoRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := &attrsRequestCounter{next: b2sim.New()}
	client, err := NewClient(ctx, "test-id", "test-key", Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.NewBucket(ctx, "empty-range", &BucketAttrs{Type: Private})
	if err != nil {
		t.Fatal(err)
	}
	// After setup, count and reject every request, including HEAD and GET.
	transport.next = bucketInfoRoundTripper(func(req *http.Request) (*http.Response, error) {
		cancel() // An unwanted request must fail the test without stranding Read.
		return badTransport{}.RoundTrip(req)
	})
	transport.calls.Store(0)
	reader := bucket.Object("not-uploaded").NewRangeReader(ctx, 0, 0)
	t.Cleanup(func() { _ = reader.Close() })
	samples := make(chan [2]int64, 1)
	reader.Progress = func(complete, total int64) {
		select {
		case samples <- [2]int64{complete, total}:
		default:
			t.Error("unexpected extra progress callback")
		}
	}
	var buf [1]byte
	if n, err := reader.Read(buf[:]); n != 0 || err != io.EOF {
		t.Errorf("Read = (%d, %v), want (0, EOF)", n, err)
	}
	select {
	case got := <-samples:
		if got != [2]int64{0, 0} {
			t.Errorf("terminal progress = %v, want [0 0]", got)
		}
	default:
		t.Error("Read returned without terminal progress")
	}
	if got := transport.calls.Load(); got != 0 {
		t.Errorf("zero-length range made %d HTTP requests, want 0", got)
	}
}

func TestReaderShortReadBackoffSequenceAndCap(t *testing.T) {
	// Pin the SDK's short-read policy from #262, including its overshoot of 10s.
	want := []time.Duration{
		1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond,
		8 * time.Millisecond, 16 * time.Millisecond, 32 * time.Millisecond,
		64 * time.Millisecond, 128 * time.Millisecond, 256 * time.Millisecond,
		512 * time.Millisecond, 1024 * time.Millisecond, 2048 * time.Millisecond,
		4096 * time.Millisecond, 8192 * time.Millisecond, 16384 * time.Millisecond,
		16384 * time.Millisecond,
	}
	timer := newTestAfter()
	var delay backoff
	for range want {
		if err := delay.wait(context.Background(), timer.wait); err != nil {
			t.Fatal(err)
		}
	}
	if got := timer.durations(); !reflect.DeepEqual(got, want) {
		t.Errorf("short-read delays = %v, want %v", got, want)
	}
}
