// Copyright 2018, the Blazer authors
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

type epair struct {
	e interface{}
	t time.Time
}

func adder(i, j interface{}) interface{} {
	a, ok := i.(int)
	if !ok {
		a = 0
	}
	b, ok := j.(int)
	if !ok {
		b = 0
	}
	return a + b
}

func TestWindows(t *testing.T) {
	// New(time.Second, time.Minute, adder) and New(time.Second, 0, adder)
	// currently panic on use because they allocate zero buckets. #176 tracks
	// validating those inputs, so this test does not assert their behavior yet.
	table := []struct {
		size, dur time.Duration
		incs      []epair
		look      time.Time
		reduce    Reducer
		want      interface{}
	}{
		{
			size: time.Minute,
			dur:  time.Second,
			incs: []epair{
				// year, month, day, hour, min, sec, nano
				{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 2, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 3, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 4, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 5, 0, time.UTC), e: 1},
			},
			look:   time.Date(2000, 1, 1, 0, 1, 0, 0, time.UTC),
			want:   5,
			reduce: adder,
		},
		{
			incs: []epair{
				// year, month, day, hour, min, sec, nano
				{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 2, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 3, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 4, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 5, 0, time.UTC), e: 1},
			},
			want:   6,
			reduce: adder,
		},
		{ // what happens if time goes backwards?
			size: time.Minute,
			dur:  time.Second,
			incs: []epair{
				{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 2, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 3, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 4, 0, time.UTC), e: 1},
				{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), e: 1},
			},
			look:   time.Date(2000, 1, 1, 0, 0, 30, 0, time.UTC),
			want:   1,
			reduce: adder,
		},
		{
			size: time.Minute,
			dur:  time.Second,
			incs: []epair{
				{t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), e: 1},
			},
			look:   time.Date(2000, 1, 1, 0, 2, 0, 0, time.UTC),
			want:   0,
			reduce: adder,
		},
	}

	for _, e := range table {
		w := New(e.size, e.dur, e.reduce)
		for _, inc := range e.incs {
			w.insertAt(inc.t, inc.e)
		}
		ct := w.reducedAt(e.look)
		if ct != e.want {
			t.Errorf("reducedAt(%v) got %v, want %v", e.look, ct, e.want)
		}
	}
}

func TestExampleCounterAndAccumulatorExpireWithFakeClock(t *testing.T) {
	// The public examples call Insert and Reduce, which use time.Now. Exercise
	// their same reducer shapes with insertAt and reducedAt for a fixed clock.
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	counter := New(time.Second, time.Millisecond, adder)
	counter.insertAt(start, 1)
	if got := counter.reducedAt(start.Add(2 * time.Second)); got != 0 {
		t.Fatalf("counter after expiry = %v, want 0", got)
	}
	accumulate := func(i, j interface{}) interface{} {
		var result []string
		if i != nil {
			result = append(result, i.([]string)...)
		}
		if j != nil {
			result = append(result, j.([]string)...)
		}
		return result
	}
	accumulator := New(time.Second, time.Millisecond, accumulate)
	accumulator.insertAt(start, []string{"this", "is", "that"})
	if got := accumulator.reducedAt(start.Add(2 * time.Second)).([]string); len(got) != 0 {
		t.Fatalf("accumulator after expiry = %v, want empty", got)
	}
}
