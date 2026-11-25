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

// Two simultaneous uploads of the same object name must both be visible in
// Status(): the status maps are keyed per writer, not by object name, so one
// transfer no longer overwrites another in the status display.
func TestRegression_ConcurrentSameNameWritersBothInStatus(t *testing.T) {
	bucket, _ := newMappingBucket(t, updateBucketJSON)
	ctx := newTestCtx(t)
	w1 := bucket.Object("same").NewWriter(ctx)
	w2 := bucket.Object("same").NewWriter(ctx)
	if _, err := w1.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := w2.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	got := len(bucket.c.Status().Writers)
	_ = w1.Close()
	_ = w2.Close()
	if got != 2 {
		t.Errorf("Status lists %d writers for two simultaneous uploads of one name, want 2", got)
	}
}
