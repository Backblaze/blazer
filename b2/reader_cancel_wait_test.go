package b2

import (
	"context"
	"errors"
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
