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
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func TestDelimitedListingsReportZeroFolderTimestamp(t *testing.T) {
	// https://www.backblaze.com/apidocs/b2-list-file-names
	// https://www.backblaze.com/apidocs/b2-list-file-versions
	const files = `[
		{"fileName":"logs/","action":"folder","uploadTimestamp":0},
		{"fileName":"zero-file","action":"upload","uploadTimestamp":0},
		{"fileName":"recent-folder/","action":"folder","uploadTimestamp":1}
	]`
	for _, test := range []struct {
		name, call string
		response   string
		list       func(context.Context, *Bucket) ([]*File, error)
	}{
		{
			name:     "file names",
			call:     "b2_list_file_names",
			response: `{"files":` + files + `,"nextFileName":null}`,
			list: func(ctx context.Context, bucket *Bucket) ([]*File, error) {
				files, _, err := bucket.ListFileNames(ctx, 10, "", "logs/", "/")
				return files, err
			},
		},
		{
			name:     "file versions",
			call:     "b2_list_file_versions",
			response: `{"files":` + files + `,"nextFileName":null,"nextFileId":null}`,
			list: func(ctx context.Context, bucket *Bucket) ([]*File, error) {
				files, _, _, err := bucket.ListFileVersions(ctx, 10, "", "", "logs/", "/")
				return files, err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			srv := b2wiretest.New(t)
			srv.Reply(test.call, http.StatusOK, test.response)
			b2, err := AuthorizeAccount(ctx, "account-id", "application-key", SetAPIBase(srv.URL()))
			if err != nil {
				t.Fatal(err)
			}
			files, err := test.list(ctx, &Bucket{ID: "bucket-id", b2: b2})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 3 {
				t.Fatalf("files = %d, want 3", len(files))
			}
			if !files[0].Timestamp.IsZero() || !files[0].Info.Timestamp.IsZero() {
				t.Errorf("folder timestamps = (%v, %v), want zero times", files[0].Timestamp, files[0].Info.Timestamp)
			}
			if want := time.Unix(0, 0); !files[1].Timestamp.Equal(want) || !files[1].Info.Timestamp.Equal(want) {
				t.Errorf("regular-file timestamps = (%v, %v), want %v", files[1].Timestamp, files[1].Info.Timestamp, want)
			}
			if want := time.UnixMilli(1); !files[2].Timestamp.Equal(want) || !files[2].Info.Timestamp.Equal(want) {
				t.Errorf("nonzero-folder timestamps = (%v, %v), want %v", files[2].Timestamp, files[2].Info.Timestamp, want)
			}
			request := srv.Last(t, test.call)
			b2wiretest.AssertJSON(t, request, "bucketId", "bucket-id")
			b2wiretest.AssertJSON(t, request, "prefix", "logs/")
			b2wiretest.AssertJSON(t, request, "delimiter", "/")
		})
	}
}
