package b2

import (
	"sync"
	"testing"
)

func TestProgressConcurrentProducersAndFinish(t *testing.T) {
	for cycle := 0; cycle < 50; cycle++ {
		var last int64
		p := newProgressReporter(func(n, total int64) {
			if n < last || n > total {
				t.Errorf("progress %d after %d total=%d", n, last, total)
			}
			last = n
		}, 32*100)
		var producers sync.WaitGroup
		for i := 0; i < 32; i++ {
			producers.Add(1)
			go func() {
				defer producers.Done()
				for j := 0; j < 100; j++ {
					p.add(1)
				}
			}()
		}
		producers.Wait()
		var closers sync.WaitGroup
		for i := 0; i < 8; i++ {
			closers.Add(1)
			go func() { defer closers.Done(); p.finish() }()
		}
		closers.Wait()
		if last != 3200 {
			t.Fatalf("final=%d", last)
		}
		select {
		case <-p.done:
		default:
			t.Fatal("owner goroutine did not exit")
		}
		p.add(100)
		p.finish()
		if last != 3200 {
			t.Fatal("callback after terminal")
		}
	}
}
