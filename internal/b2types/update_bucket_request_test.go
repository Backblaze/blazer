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

package b2types

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUpdateBucketRequestClearsLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		rules []LifecycleRule
		want  []byte
	}{
		{
			name:  "empty",
			rules: []LifecycleRule{},
			want:  []byte(`"lifecycleRules":[]`),
		},
		{
			name: "populated",
			rules: []LifecycleRule{{
				Prefix: "logs/",
			}},
			want: []byte(`"lifecycleRules":[{"fileNamePrefix":"logs/"}]`),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents, err := json.Marshal(UpdateBucketRequest{LifecycleRules: test.rules})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(contents, test.want) {
				t.Errorf("json.Marshal(UpdateBucketRequest{LifecycleRules: %#v}) = %s; want %s", test.rules, contents, test.want)
			}
		})
	}
}
