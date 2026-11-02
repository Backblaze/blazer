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
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// io.Copy (and any caller) gives ReadFrom a reader positioned part-way in;
// only the remaining bytes may be uploaded.
func TestWriterReadFromSeekableReaderUsesCurrentOffset(t *testing.T) {
	const payload = "0123456789"
	for _, tc := range []struct {
		name  string
		csize int
	}{
		{"single request", 100},
		{"large file", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, bucket := newSHA1WireBucket(t)
			srv.Reply("upload_file", 200, `{"fileId":"f","fileName":"o","action":"upload"}`)
			srv.Reply("b2_start_large_file", 200, `{"fileId":"large-1"}`)
			srv.Reply("upload_part", 200, `{}`)
			srv.Reply("b2_finish_large_file", 200, `{"fileId":"large-1","fileName":"o","action":"upload"}`)
			r := strings.NewReader(payload)
			if _, err := r.Seek(3, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			w := bucket.Object("o").NewWriter(context.Background())
			w.ChunkSize = tc.csize
			n, err := w.ReadFrom(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if want := int64(len(payload) - 3); n != want {
				t.Errorf("ReadFrom = %d bytes, want %d", n, want)
			}
			var got []byte
			if tc.csize >= len(payload) {
				got = srv.Last(t, "upload_file").Body
				// The single-request upload appends the 40-character SHA1 after the data.
				if len(got) >= 40 {
					got = got[:len(got)-40]
				}
			} else {
				for i := 1; i <= 3; i++ {
					for _, p := range srv.Calls("upload_part") {
						if p.Header.Get("X-Bz-Part-Number") == string(rune('0'+i)) {
							body := p.Body
							if len(body) >= 40 {
								body = body[:len(body)-40] // trailing SHA1
							}
							got = append(got, body...)
						}
					}
				}
			}
			if !bytes.Equal(got, []byte(payload[3:])) {
				t.Errorf("uploaded %q, want the remaining %q", got, payload[3:])
			}
		})
	}
}
