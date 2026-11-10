package base

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestDownloadContentLengthFitsNativeInt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		length  int64
		maxInt  int64
		want    int64
		wantErr bool
	}{
		{"32-bit boundary", math.MaxInt32, math.MaxInt32, math.MaxInt32, false},
		{"32-bit first larger length", 2147483648, math.MaxInt32, math.MaxInt32, false},
		{"32-bit three GiB", 3221225472, math.MaxInt32, math.MaxInt32, false},
		{"64-bit accepts large file", 3221225472, math.MaxInt64, 3221225472, false},
		{"64-bit boundary", math.MaxInt64, math.MaxInt64, math.MaxInt64, false},
		{"negative", -1, math.MaxInt64, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
			resp.Header.Set("Content-Length", fmt.Sprint(tc.length))
			got, err := newFileReaderWithMaxLength(resp, tc.maxInt)
			if (err != nil) != tc.wantErr {
				t.Fatalf("newFileReaderWithMaxLength(%d, %d) error = %v, want error %v", tc.length, tc.maxInt, err, tc.wantErr)
			}
			if err == nil {
				defer func() {
					if err := got.Close(); err != nil {
						t.Error(err)
					}
				}()
				want := tc.want
				if strconv.IntSize == 32 && want > math.MaxInt32 {
					want = math.MaxInt32
				}
				if int64(got.ContentLength) != want {
					t.Fatalf("content length = %d, want %d", got.ContentLength, want)
				}
			}
		})
	}
}

func TestFileReaderClosesBodyOnHeaderAndStatusErrors(t *testing.T) {
	for _, tc := range []struct {
		name, length string
		status       int
	}{
		{"bad length", "not-a-number", http.StatusOK},
		{"negative length", "-1", http.StatusOK},
		{"missing length", "", http.StatusOK},
		{"int64 overflow", "9223372036854775808", http.StatusOK},
		{"bad metadata escape", "0", http.StatusOK},
		{"not found", "0", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &closeTrackingBody{Reader: strings.NewReader(`{"status":404,"code":"not_found","message":"missing"}`)}
			req, err := http.NewRequest(http.MethodGet, "https://example.invalid/b2api/v4/b2_download_file_by_id?fileId=id", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Blazer-Method", "b2_download_file_by_id")
			resp := &http.Response{StatusCode: tc.status, Status: http.StatusText(tc.status), Header: make(http.Header), Body: body, Request: req}
			resp.Header.Set("Content-Length", tc.length)
			if tc.name == "bad metadata escape" {
				resp.Header.Set("X-Bz-Info-%GG", "bad")
			}
			reader, err := newFileReader(resp)
			if err == nil || reader != nil || !body.closed {
				t.Fatalf("newFileReader = (%v, %v), body closed=%v; want error and close", reader, err, body.closed)
			}
		})
	}
}

func TestFileReaderLargeContentLength(t *testing.T) {
	// HEAD has no body; a GET can be opened and read incrementally without
	// allocating the declared length. Model a 32-bit int on every host.
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			payload := ""
			if method == http.MethodGet {
				payload = "prefix"
			}
			body := &closeTrackingBody{Reader: strings.NewReader(payload)}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{
				"Content-Length": {"3221225472"},
				"X-Bz-File-Id":   {"large-file-id"},
			}, Body: body}
			reader, err := newFileReaderWithMaxLength(resp, math.MaxInt32)
			if err != nil {
				t.Fatalf("%s with 3 GiB Content-Length: %v", method, err)
			}
			defer reader.Close()
			if body.closed || reader.ContentLength != math.MaxInt32 || reader.ID != "large-file-id" {
				t.Fatalf("%s: length=%d id=%q closed=%t, want 2147483647, large-file-id, false", method, reader.ContentLength, reader.ID, body.closed)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(reader, got); err != nil || string(got) != payload {
				t.Fatalf("%s prefix = %q, %v; want %q, nil", method, got, err, payload)
			}
			if err := reader.Close(); err != nil || !body.closed {
				t.Fatalf("%s Close = %v, closed=%t; want nil, true", method, err, body.closed)
			}
		})
	}
}

func TestFileReaderCompatibilityLengthDoesNotLimitBody(t *testing.T) {
	// A small int limit makes it practical to read beyond the compatibility
	// field. Saturation must never become an io.LimitReader around the body.
	body := &closeTrackingBody{Reader: strings.NewReader("abc")}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": {"3"}}, Body: body}
	reader, err := newFileReaderWithMaxLength(resp, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != "abc" || reader.ContentLength != 2 {
		t.Fatalf("read=%q error=%v length=%d, want abc, nil, 2", got, err, reader.ContentLength)
	}
	if err := reader.Close(); err != nil || !body.closed {
		t.Fatalf("Close=%v closed=%t, want nil, true", err, body.closed)
	}
}

func TestRetryAfterLargeValueIsBoundedBeforeNativeIntConversion(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"7", 7},
		{"2147483648", 30},
		{"99999999999", 30},
		{"9223372036854775807", 30},
		{"-1", 0},
	} {
		got, err := parseRetryAfterSeconds(tc.value)
		if err != nil || got != tc.want {
			t.Errorf("parseRetryAfterSeconds(%q) = (%d, %v), want %d", tc.value, got, err, tc.want)
		}
	}
}
