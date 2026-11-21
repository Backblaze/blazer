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
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReaderCancellationReleasesChunkWaiter(t *testing.T) {
	for _, closeReader := range []bool{false, true} {
		name := "cancel"
		if closeReader {
			name = "Close"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			r := &Reader{ctx: ctx, cancel: cancel, chunks: make(map[int]*rchunk), o: &Object{b: &Bucket{c: &Client{}}}}
			r.rcond = sync.NewCond(&r.rmux)
			// Isolate the waiter from download workers, which can otherwise
			// broadcast on cancellation and conceal the missing wakeup.
			r.init.Do(func() {})
			t.Cleanup(func() {
				cancel()
				r.rmux.Lock()
				r.rcond.Broadcast()
				r.rmux.Unlock()
			})
			before := chunkWaitersParked()
			done := make(chan error, 1)
			go func() { _, err := r.curChunk(); done <- err }()
			if !awaitCond(func() bool { return newChunkWaiterParked(before) }) {
				t.Fatal("chunk waiter did not enter Cond.Wait")
			}
			if closeReader {
				if !closeWithin(r, time.Second) {
					t.Fatal("Close did not return within one second")
				}
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("curChunk returned %v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("curChunk did not return after cancellation")
			}
			if !awaitCond(func() bool { return !newChunkWaiterParked(before) }) {
				t.Fatal("chunk waiter remained blocked in Cond.Wait after cancellation")
			}
		})
	}
}

// Stress cancellation while a real download is waiting for a chunk.
func TestRegression_NoChunkWaiterLeftBlockedAfterClose(t *testing.T) {
	const readers = 1000
	bucket := blockingDownload(t)
	before := goroutinesIn(chunkWaiterSymbol)
	for i := 0; i < readers; i++ {
		r := newSlowReader(bucket, newTestCtx(t))
		done := make(chan any, 1)
		parked := chunkWaitersParked()
		go func() { done <- recoverRead(r) }()
		if !awaitCond(func() bool { return newChunkWaiterParked(parked) }) {
			closeWithin(r, 3*time.Second)
			t.Fatalf("reader %d: the Read never parked waiting for a chunk", i)
		}
		if i == 0 && goroutinesIn(chunkWaiterSymbol) <= before {
			// The final count would be 0 <= 0 and pass vacuously if the symbol were renamed.
			closeWithin(r, 3*time.Second)
			t.Fatalf("goroutine dump does not contain %q while a Read is parked in it; the symbol was renamed and this test would pass vacuously", chunkWaiterSymbol)
		}
		if !closeWithin(r, 3*time.Second) {
			t.Fatalf("reader %d: Close did not return", i)
		}
		select {
		case p := <-done:
			if p != nil {
				t.Fatalf("reader %d: Read panicked: %v", i, p)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("reader %d: Read did not return after Close", i)
		}
	}
	var now int
	if !awaitCond(func() bool { now = goroutinesIn(chunkWaiterSymbol); return now <= before }) {
		t.Errorf("%d chunk-waiter goroutine(s) still blocked after %d closed readers (before: %d)", now-before, readers, before)
	}
}

// chunkWaiterSymbol is the stack frame whose goroutines the test counts.
const chunkWaiterSymbol = "(*Reader).curChunk"

func goroutinesIn(fn string) int {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), fn)
}
