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
	"errors"
	"sync"
	"testing"
	"time"
)

type c16OffsetBucket struct {
	beBucketInterface
	offsets chan int64
}

func (b c16OffsetBucket) downloadFileByName(_ context.Context, _ string, offset, _ int64, _ bool, _ *SSECKey) (beFileReaderInterface, error) {
	b.offsets <- offset
	return nil, errors.New("offset captured")
}

// The old multiplication overflows only with 32-bit int. CI executes this test
// on linux/386; a 64-bit run alone cannot detect the regression.
func TestFindingC16_ChunkOffsetMultipliedIn64Bit(t *testing.T) {
	// The Reader cancels its own context when the fake download fails below, so
	// the test must not wait on that context: it keeps its own timer.
	ctx, cancel := context.WithCancel(context.Background())
	offsets := make(chan int64, 1)
	r := &Reader{
		ctx: ctx, cancel: cancel,
		o:     &Object{b: &Bucket{b: c16OffsetBucket{offsets: offsets}}},
		csize: 10_000_000, chwid: 300, chbuf: make(chan *rchunk, 1),
	}
	r.rcond = sync.NewCond(&r.rmux)
	r.thread()
	defer func() {
		cancel()
		r.wg.Wait()
	}()
	r.chbuf <- &rchunk{}
	select {
	case got := <-offsets:
		if want := int64(3_000_000_000); got <= 0 || got != want {
			t.Fatalf("chunk offset = %d, want positive %d", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the chunk offset")
	}
}
