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

package sync

import (
	"testing"
	"time"

	"github.com/Backblaze/blazer/b2"
)

func TestShouldTransfer(t *testing.T) {
	mtime := time.Unix(1_700_000_000, 123_456_789)
	local := localFile{size: 12, modTime: mtime, sha1: "matching-sha1"}
	baseDest := b2.Attrs{Size: 12, LastModified: mtime.Truncate(time.Millisecond), SHA1: local.sha1}
	tests := []struct {
		name string
		mode CompareMode
		dest *b2.Attrs
		want bool
	}{
		{"missing modtime", CompareModTime, nil, true},
		{"missing size", CompareSize, nil, true},
		{"missing none", CompareNone, nil, true},
		{"missing sha1", CompareSHA1, nil, true},
		{"modtime equal to millisecond", CompareModTime, &baseDest, false},
		{"modtime size differs", CompareModTime, &b2.Attrs{Size: 13, LastModified: baseDest.LastModified}, true},
		{"modtime differs by millisecond", CompareModTime, &b2.Attrs{Size: 12, LastModified: baseDest.LastModified.Add(time.Millisecond)}, true},
		{"size equal", CompareSize, &baseDest, false},
		{"size differs", CompareSize, &b2.Attrs{Size: 13}, true},
		{"none existing", CompareNone, &b2.Attrs{Size: 13}, false},
		{"sha1 equal", CompareSHA1, &baseDest, false},
		{"sha1 differs", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "different"}, true},
		{"sha1 none same size", CompareSHA1, &b2.Attrs{Size: 12, SHA1: "none"}, false},
		{"sha1 none different size", CompareSHA1, &b2.Attrs{Size: 13, SHA1: "none"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldTransfer(local, test.dest, test.mode); got != test.want {
				t.Fatalf("shouldTransfer() = %t, want %t", got, test.want)
			}
		})
	}
}
