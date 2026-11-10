package sync

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestSyncEmptyTreeUsesBoundedWorkerPool(t *testing.T) {
	const child = "BLAZER_SYNC_WORKER_POOL_CHILD"
	if os.Getenv(child) != "1" {
		// An unbounded worker-launch loop ignores cancellation. Keep it in a
		// child so a failed resource assertion cannot leak workers into the suite.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSyncEmptyTreeUsesBoundedWorkerPool$")
		command.Env = append(os.Environ(), child+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated Sync worker pool: %v\n%s", err, output)
		}
		return
	}

	bucket, wire := newSyncWire(t)
	root := t.TempDir()
	baseline := runtime.NumGoroutine()
	done := make(chan struct{})
	var result *Result
	var err error
	go func() {
		result, err = Sync(context.Background(), bucket, "p", root, WithConcurrency(1))
		close(done)
	}()
	for {
		select {
		case <-done:
			if err != nil || result == nil || len(result.Uploaded) != 0 || len(result.Skipped) != 0 || len(result.Failed) != 0 || result.BytesUploaded != 0 {
				t.Fatalf("empty Sync = (%+v, %v), want an empty result and nil error", result, err)
			}
			wire.mu.Lock()
			defer wire.mu.Unlock()
			if len(wire.uploads) != 0 {
				t.Fatalf("upload requests = %d, want 0", len(wire.uploads))
			}
			return
		default:
			// Allow incidental HTTP/runtime goroutines, but fail an unbounded
			// pool by resource count instead of waiting for the runner timeout.
			if extra := runtime.NumGoroutine() - baseline; extra > 64 {
				t.Fatalf("Sync launched %d extra goroutines with concurrency 1; want a bounded worker pool", extra)
			}
			runtime.Gosched()
		}
	}
}
