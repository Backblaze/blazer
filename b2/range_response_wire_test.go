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
	"strings"
	"testing"

	"github.com/Backblaze/blazer/base"
)

func TestRangeReaderRejectsFullObjectResponse(t *testing.T) {
	const object = "abcdef"
	bucket, ranges := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Length": {"6"}},
			Body:       io.NopCloser(strings.NewReader(object)),
			Request:    req,
		}, nil
	})

	ctx := context.Background()
	r := bucket.Object("object").NewRangeReader(ctx, 2, 2)
	r.ChunkSize = 3
	t.Cleanup(func() { _ = r.Close() })

	n, err := io.ReadFull(r, make([]byte, 3))
	if n != 0 {
		t.Fatalf("NewRangeReader read %d bytes from a rejected full-object response, want 0", n)
	}
	if err == nil {
		t.Fatal("NewRangeReader accepted a full-object response for a range request")
	}
	if !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader error = %v, want an invalid range response", err)
	}
	if got, want := *ranges, []string{"bytes=2-3"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Range requests = %v, want %v", got, want)
	}
}

func TestNewReaderAcceptsWholeFileResponse(t *testing.T) {
	const object = "abcdef"
	bucket, ranges := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Range") == "bytes=0-6" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Length": {"6"}},
				Body:       io.NopCloser(strings.NewReader(object)),
				Request:    req,
			}, nil
		}
		return drJSON(req, http.StatusRequestedRangeNotSatisfiable, map[string]interface{}{
			"status": http.StatusRequestedRangeNotSatisfiable, "code": "range_not_satisfiable", "message": "past end",
		})
	})

	r := bucket.Object("object").NewReader(context.Background())
	r.ChunkSize = 7
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != object {
		t.Errorf("NewReader returned %q, want %q", got, object)
	}
	if want := []string{"bytes=0-6", "bytes=7-13"}; len(*ranges) != len(want) || (*ranges)[0] != want[0] || (*ranges)[1] != want[1] {
		t.Errorf("Range requests = %v, want %v", *ranges, want)
	}
}

func TestRangeReaderRejectsOversizedOffsetZeroResponse(t *testing.T) {
	const object = "abcdef"
	bucket, ranges := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Length": {"6"}},
			Body:       io.NopCloser(strings.NewReader(object)),
			Request:    req,
		}, nil
	})

	r := bucket.Object("object").NewRangeReader(context.Background(), 0, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if len(got) != 0 {
		t.Fatalf("NewRangeReader returned %q from an oversized HTTP 200 response, want no bytes", got)
	}
	if !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader error = %v, want an invalid range response", err)
	}
	if got, want := *ranges, []string{"bytes=0-1"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Range requests = %v, want %v", got, want)
	}
}

func TestRangeReaderRejectsUnboundedFullObjectResponse(t *testing.T) {
	const object = "abcdef"
	bucket, _ := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Length": {"6"}},
			Body:       io.NopCloser(strings.NewReader(object)),
			Request:    req,
		}, nil
	})

	r := bucket.Object("object").NewRangeReader(context.Background(), 0, -1)
	r.ChunkSize = 3
	t.Cleanup(func() { _ = r.Close() })
	got := make([]byte, 4)
	n, err := io.ReadFull(r, got)
	if n != 0 {
		t.Fatalf("NewRangeReader returned %q (%d bytes) from an oversized HTTP 200 response, want no bytes", got[:n], n)
	}
	if !errors.Is(err, base.ErrInvalidRangeResponse) {
		t.Fatalf("NewRangeReader error = %v, want an invalid range response", err)
	}
}

func TestRangeReaderRejectsWrongContentRange(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Length": {"2"},
				"Content-Range":  {"bytes 0-1/6"},
			},
			Body:    io.NopCloser(strings.NewReader("ab")),
			Request: req,
		}, nil
	})

	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	if _, err := io.ReadFull(r, make([]byte, 2)); err == nil {
		t.Fatal("NewRangeReader accepted a response whose Content-Range starts before the requested offset")
	}
}

func TestRangeReaderCapsOversizedRangeResponse(t *testing.T) {
	bucket, _ := newRangeReaderWireBucket(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Length": {"6"},
				"Content-Range":  {"bytes 2-3/6"},
			},
			Body:    io.NopCloser(strings.NewReader("cdefgh")),
			Request: req,
		}, nil
	})

	r := bucket.Object("object").NewRangeReader(context.Background(), 2, 2)
	t.Cleanup(func() { _ = r.Close() })
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := "cd"; string(got) != want {
		t.Errorf("NewRangeReader returned %q, want %q", got, want)
	}
}

func newRangeReaderWireBucket(t *testing.T, download func(*http.Request) (*http.Response, error)) (*Bucket, *[]string) {
	t.Helper()
	var ranges []string
	transport := drTransport(func(req *http.Request) (*http.Response, error) {
		switch req.Header.Get("X-Blazer-Method") {
		case "b2_authorize_account":
			return drJSON(req, http.StatusOK, map[string]interface{}{
				"accountId": "account", "authorizationToken": "token",
				"apiInfo": map[string]interface{}{"storageApi": map[string]interface{}{
					"apiUrl": "https://b2.test", "downloadUrl": "https://b2.test",
				}},
			})
		case "b2_list_buckets":
			return drJSON(req, http.StatusOK, map[string]interface{}{"buckets": []interface{}{map[string]interface{}{
				"bucketId": "bucket-id", "bucketName": "bucket", "bucketType": "allPrivate", "bucketInfo": map[string]string{},
			}}})
		case "b2_download_file_by_name":
			ranges = append(ranges, req.Header.Get("Range"))
			return download(req)
		default:
			return drJSON(req, http.StatusNotFound, map[string]interface{}{"status": 404, "code": "not_found", "message": "unexpected"})
		}
	})

	client, err := NewClient(context.Background(), "account", "key", APIBase("https://b2.test"), Transport(transport))
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.Bucket(context.Background(), "bucket")
	if err != nil {
		t.Fatal(err)
	}
	return bucket, &ranges
}
