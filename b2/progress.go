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

import "sync"

// progressReporter coalesces meter updates without running user code in a
// transfer goroutine or while holding a lock. Only run owns completed and total.
type progressReporter struct {
	callback func(int64, int64)
	total    int64
	mu       sync.Mutex
	pending  int64
	stopped  bool
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

func newProgressReporter(callback func(int64, int64), total int64) *progressReporter {
	if callback == nil {
		return nil
	}
	p := &progressReporter{
		callback: callback,
		total:    total,
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *progressReporter) add(n int64) {
	if p == nil || n <= 0 {
		return
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.pending += n
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *progressReporter) take() int64 {
	p.mu.Lock()
	n := p.pending
	p.pending = 0
	p.mu.Unlock()
	return n
}

func (p *progressReporter) run() {
	defer close(p.done)
	var completed int64
	for {
		select {
		case <-p.wake:
			if n := p.take(); n > 0 {
				completed += n
				p.deliver(completed)
			}
		case <-p.stop:
			completed += p.take()
			// This terminal call is made even if the last meter update was
			// already delivered, and even for a zero-byte transfer.
			p.deliver(completed)
			return
		}
	}
}

func (p *progressReporter) deliver(completed int64) {
	p.callback(completed, p.total)
}

func (p *progressReporter) finish() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.stopped = true
		p.mu.Unlock()
		close(p.stop)
	})
	<-p.done
}
