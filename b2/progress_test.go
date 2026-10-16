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
	payload := []byte("progress callback payload")

	t.Run("upload", func(t *testing.T) {
		var mu sync.Mutex
		var samples []progressSample
		writer := bucket.Object("upload").NewWriter(ctx, WithProgress(func(complete, total int64) {
			mu.Lock()
			samples = append(samples, progressSample{complete, total})
			mu.Unlock()
		}))
		if _, err := writer.ReadFrom(bytes.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		assertProgressSamples(t, progressSnapshot(&mu, samples), int64(len(payload)))
	})

	t.Run("download", func(t *testing.T) {
		root.bucketMap["progress-test"]["download"] = string(payload)
		var mu sync.Mutex
		var samples []progressSample
		reader := bucket.Object("download").NewRangeReader(ctx, 0, int64(len(payload)))
		reader.ChunkSize = 3
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

		assertProgressSamples(t, progressSnapshot(&mu, samples), int64(len(payload)))
	})
}

func progressSnapshot(mu *sync.Mutex, samples []progressSample) []progressSample {
	mu.Lock()
	defer mu.Unlock()
	return append([]progressSample(nil), samples...)
}

func assertProgressSamples(t *testing.T, samples []progressSample, wantTotal int64) {
	t.Helper()
	if len(samples) == 0 {
		t.Fatal("progress callback was not called")
	}
	var previous int64
	for i, sample := range samples {
		if sample.complete < previous {
			t.Fatalf("sample %d complete = %d, previous = %d", i, sample.complete, previous)
		}
		if sample.total != wantTotal {
			t.Fatalf("sample %d total = %d, want %d", i, sample.total, wantTotal)
		}
		previous = sample.complete
	}
	if got := samples[len(samples)-1].complete; got != wantTotal {
		t.Fatalf("final complete = %d, want %d", got, wantTotal)
	}
}
