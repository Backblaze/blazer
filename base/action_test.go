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

import "testing"

func TestActionConnectionFaultUpload(t *testing.T) {
	tests := []struct {
		name string
		err  b2err
		want ErrAction
	}{
		{
			name: "upload file",
			err:  b2err{method: "b2_upload_file", retry: 1},
			want: AttemptNewUpload,
		},
		{
			name: "upload part",
			err:  b2err{method: "b2_upload_part", retry: 1},
			want: AttemptNewUpload,
		},
		{
			name: "download",
			err:  b2err{method: "b2_download_file_by_name", retry: 1},
			want: Retry,
		},
		{
			name: "no method",
			err:  b2err{retry: 1},
			want: Retry,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Action(test.err); got != test.want {
				t.Errorf("Action(%#v) = %v; want %v", test.err, got, test.want)
			}
		})
	}
}
