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
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Backblaze/blazer/base"
	"github.com/Backblaze/blazer/internal/b2wiretest"
)

func newRangeReaderWireBucket(t *testing.T, download func(b2wiretest.Request) b2wiretest.Response) (*Bucket, *b2wiretest.Server) {
	t.Helper()
	srv := b2wiretest.New(t)
	srv.Handle("object", download)
	return newListingWireBucket(t, context.Background(), srv), srv
}

func assertRangeCalls(t *testing.T, srv *b2wiretest.Server, want ...string) {
	t.Helper()
	calls := srv.Calls("object")
	if len(calls) != len(want) {
		t.Fatalf("range calls = %d, want %d", len(calls), len(want))
	}
	for i, call := range calls {
		b2wiretest.AssertHeader(t, call, "Range", want[i])
	}
}

func TestRangeReaderRejectsFullObjectResponse(t *testing.T) {
	bucket, srv := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("abcdef", "")
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	r.ChunkSize = 3
	t.Cleanup(func() { _ = r.Close() })

	n, err := io.ReadFull(r, make([]byte, 3))
	if n != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%d bytes, %v), want (0, invalid range response)", n, err)
	}
	assertRangeCalls(t, srv, "bytes=2-3")
}

func TestRangeReaderRejectsNonPartialRangeResponse(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		response := b2wiretest.RangeDownload("cd", "")
		response.Header.Set("Content-Range", "bytes 2-3/6")
		return response
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
	}
}

func TestNewReaderAcceptsWholeFileResponse(t *testing.T) {
	bucket, srv := newRangeReaderWireBucket(t, func(req b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("abcdef", req.Header.Get("Range"))
	})
	r := bucket.Object("object").NewReader(context.Background())
	r.ChunkSize = 7
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "abcdef" {
		t.Fatalf("NewReader = (%q, %v), want (abcdef, nil)", got, err)
	}
	assertRangeCalls(t, srv, "bytes=0-6", "bytes=7-13")
}

func TestRangeReaderRejectsOversizedOffsetZeroResponse(t *testing.T) {
	bucket, srv := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("abcdef", "")
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 0, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
	}
	assertRangeCalls(t, srv, "bytes=0-1")
}

func TestRangeReaderRejectsUnboundedFullObjectResponse(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("abcdef", "")
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 0, -1)
	r.ChunkSize = 3
	t.Cleanup(func() { _ = r.Close() })
	got := make([]byte, 4)
	n, err := io.ReadFull(r, got)
	if n != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got[:n], err)
	}
}

func TestRangeReaderRejectsWrongContentRange(t *testing.T) {
	for _, span := range []string{"bytes 0-1/6", "bytes 2-999999/10"} {
		t.Run(span, func(t *testing.T) {
			bucket, _ := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
				response := b2wiretest.RangeDownload("abcdef", "bytes=2-3")
				response.Header.Set("Content-Range", span)
				return response
			})
			r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
			t.Cleanup(func() { _ = r.Close() })
			got, err := io.ReadAll(r)
			if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
				t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
			}
		})
	}
}

func TestRangeReaderRejectsOversizedPartialResponse(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		response := b2wiretest.RangeDownload("abcdef", "bytes=2-3")
		response.Body = "cdefgh"
		response.Header.Set("Content-Length", "6")
		return response
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
	}
}

func TestRangeReaderAcceptsMatchingContentLength(t *testing.T) {
	bucket, srv := newRangeReaderWireBucket(t, func(req b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.RangeDownload("abcdef", req.Header.Get("Range"))
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "cd" {
		t.Fatalf("NewRangeReader = (%q, %v), want (cd, nil)", got, err)
	}
	assertRangeCalls(t, srv, "bytes=2-3")
}

func TestRangeReaderRejectsMissingContentRange(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.Response{Status: http.StatusPartialContent, Header: http.Header{"Content-Length": {"2"}}, Body: "cd"}
	})
	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
	}
}

func TestRangeReaderRejectsChunkedWholeFileResponse(t *testing.T) {
	bucket, srv := newRangeReaderWireBucket(t, func(b2wiretest.Request) b2wiretest.Response {
		return b2wiretest.Response{Status: http.StatusOK, Header: http.Header{"Transfer-Encoding": {"chunked"}}, Body: "aa"}
	})
	probe, err := http.Get(srv.URL() + "/file/bucket/object")
	if err != nil {
		t.Fatal(err)
	}
	_ = probe.Body.Close()
	if got := probe.Header.Get("Content-Length"); got != "" {
		t.Fatalf("chunked fixture Content-Length = %q, want absent", got)
	}
	r := bucket.Object("object").NewRangeReader(context.Background(), 0, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 || !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader = (%q, %v), want (empty, invalid range response)", got, err)
	}
}
