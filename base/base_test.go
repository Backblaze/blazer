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

package base

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	table := []struct {
		name  string
		retry int
		want  time.Duration
	}{
		{
			name:  "small retry value passes through unscaled",
			retry: 5,
			want:  5 * time.Second,
		},
		{
			name:  "retry value at the cap passes through unscaled",
			retry: 30,
			want:  30 * time.Second,
		},
		{
			name:  "large retry value is clamped to the cap",
			retry: 5000,
			want:  30 * time.Second,
		},
	}

	for _, e := range table {
		got := Backoff(b2err{retry: e.retry})
		if got != e.want {
			t.Errorf("%s: Backoff(b2err{retry: %d}): got %v, want %v", e.name, e.retry, got, e.want)
		}
	}
}
