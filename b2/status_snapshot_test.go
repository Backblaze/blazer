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

import "testing"

func TestStatusSnapshotsDoNotShareMutableState(t *testing.T) {
	counter := newMethodCounter(0, 0)
	counter.record(method{name: "list", status: 200})
	wm := &meteredReader{size: 4}
	wm.read.Store(1)
	rm := &meteredReader{size: 4}
	rm.read.Store(2)
	w := &Writer{smap: map[int]*meteredReader{1: wm}}
	r := &Reader{smap: map[int]*meteredReader{0: rm}}
	client := &Client{sMethods: []methodCounter{counter}, sWriters: map[*Writer]string{w: "writer"}, sReaders: map[*Reader]string{r: "reader"}}
	first := client.Status()
	if first.RPCs[0].CountByMethod()["list"] != 1 || first.Writers["writer"].Progress[0] != 0.25 || first.Readers["reader"].Progress[0] != 0.5 {
		t.Fatal("unexpected initial snapshot")
	}
	clear(first.RPCs[0])
	first.Writers["writer"].Progress[0] = 1
	first.Readers["reader"].Progress[0] = 1
	delete(first.Writers, "writer")
	delete(first.Readers, "reader")
	second := client.Status()
	if got := second.RPCs[0].CountByMethod()["list"]; got != 1 {
		t.Errorf("all-time list count = %d, want 1", got)
	}
	if got := second.Writers["writer"].Progress[0]; got != 0.25 {
		t.Errorf("writer progress = %v, want 0.25", got)
	}
	if got := second.Readers["reader"].Progress[0]; got != 0.5 {
		t.Errorf("reader progress = %v, want 0.5", got)
	}
}
