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

import (
	"context"
	"strings"
	"testing"
)

func TestFindingC37_VerifyBeforeReadDoesNotPanic(t *testing.T) {
	for _, tc := range []struct {
		name        string
		badKeyFirst bool
	}{
		{name: "before read"},
		{name: "after rejected customer key", badKeyFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, bucket := newSHA1WireBucket(t)
			var reader *Reader
			if tc.badKeyFirst {
				reader = bucket.Object("object").NewReader(context.Background(), &SSECKey{})
				if n, err := reader.Read(make([]byte, 1)); n != 0 || err == nil || !strings.Contains(err.Error(), "NewSSECKey") {
					t.Fatalf("Read with invalid customer key = (%d, %v), want key validation error", n, err)
				}
			} else {
				reader = bucket.Object("object").NewReader(context.Background())
			}
			defer func() {
				if err := reader.Close(); err != nil {
					t.Errorf("Close = %v, want nil", err)
				}
			}()
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("Verify panicked: %v", p)
				}
			}()
			if err, ok := reader.Verify(); err != nil || ok {
				t.Errorf("Verify = (%v, %t), want (nil, false)", err, ok)
			}
		})
	}
}
