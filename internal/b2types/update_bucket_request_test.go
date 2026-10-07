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

func TestUpdateBucketRequestLifecycleRules(t *testing.T) {
	empty := []LifecycleRule{}
	populated := []LifecycleRule{{Prefix: "logs/"}}
	tests := []struct {
		name    string
		rules   *[]LifecycleRule
		want    string // substring that must be present
		notWant string // substring that must be absent
	}{
		{name: "nil pointer omits the field", rules: nil, notWant: `lifecycleRules`},
		{name: "pointer to an empty slice sends []", rules: &empty, want: `"lifecycleRules":[]`},
		{name: "populated", rules: &populated, want: `"lifecycleRules":[{"fileNamePrefix":"logs/"}]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents, err := json.Marshal(UpdateBucketRequest{LifecycleRules: test.rules})
			if err != nil {
				t.Fatal(err)
			}
			if test.want != "" && !bytes.Contains(contents, []byte(test.want)) {
				t.Errorf("request = %s; want it to contain %s", contents, test.want)
			}
			if test.notWant != "" && bytes.Contains(contents, []byte(test.notWant)) {
				t.Errorf("request = %s; want it to omit %s", contents, test.notWant)
			}
		})
	}
}
