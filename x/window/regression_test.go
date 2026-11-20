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
	"testing"
	"time"
)

// A time bucket index must be reduced modulo the bucket count in 64-bit
// arithmetic before converting to int: `int(x) % n` truncates x to 32 bits on
// 32-bit platforms first and gives a negative (out of range) index for
// millisecond windows.

// With three one-millisecond buckets, the absolute bucket 2147483648 (the first
// that does not fit in int32) belongs in bucket 2147483648 % 3 = 2.
func TestRegression_BucketIndexReducedBeforeIntConversion(t *testing.T) {
	const abs = int64(1) << 31
	at := time.Unix(0, abs*int64(time.Millisecond))
	sum := func(i, j interface{}) interface{} {
		a, _ := i.(int)
		b, _ := j.(int)
		return a + b
	}
	w := New(3*time.Millisecond, time.Millisecond, sum)
	if got := w.bucket(at); got != 2 {
		t.Errorf("bucket(%v) = %d, want 2", at, got)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("window operations at bucket %d panicked: %v", abs, r)
		}
	}()
	// Insert in the previous millisecond, then at the boundary: the sweep that
	// expires buckets walks the same indices as bucket().
	w.insertAt(at.Add(-time.Millisecond), 1)
	w.insertAt(at, 10)
	if got := w.reducedAt(at); got != 11 {
		t.Errorf("reduced value at the boundary = %v, want 11", got)
	}
}
