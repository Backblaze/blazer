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

package window

import (
	"sync"
	"testing"
)

// Goroutines that call Insert at the same time must all be counted. A caller
// that read the clock slightly earlier but took the lock later must not be
// mistaken for the clock going backwards and wipe the window.
func TestConcurrentInsertKeepsAll(t *testing.T) {
	for r := 0; r < 200; r++ {
		w := New(60e9, 1e9, func(a, b interface{}) interface{} {
			x, _ := a.(int)
			y, _ := b.(int)
			return x + y
		})
		var wg sync.WaitGroup
		const n = 64
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); w.Insert(1) }()
		}
		wg.Wait()
		if got := w.Reduce(); got != n {
			t.Fatalf("round %d: got %v want %d", r, got, n)
		}
	}
}
